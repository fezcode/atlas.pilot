package window

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-vgo/robotgo"
	"github.com/tailscale/win"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	procEnumWindows          = user32.NewProc("EnumWindows")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procEnumDisplayMonitors  = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	procGetWindowRect        = user32.NewProc("GetWindowRect")
)

const (
	WM_CLOSE = 0x0010
)

// WindowInfo represents basic information about an open window
type WindowInfo struct {
	Handle       string `json:"handle"`
	PID          int    `json:"pid"`
	Title        string `json:"title"`
	Controllable bool   `json:"controllable"`
}

type MonitorInfo struct {
	Handle win.HMONITOR
	Bounds win.RECT
}

func getMonitors() []MonitorInfo {
	var monitors []MonitorInfo
	cb := syscall.NewCallback(func(hMonitor win.HMONITOR, hdcMonitor win.HDC, lprcMonitor *win.RECT, dwData uintptr) uintptr {
		var mi struct {
			Size    uint32
			Monitor win.RECT
			Work    win.RECT
			Flags   uint32
		}
		mi.Size = uint32(unsafe.Sizeof(mi))
		procGetMonitorInfoW.Call(uintptr(hMonitor), uintptr(unsafe.Pointer(&mi)))
		monitors = append(monitors, MonitorInfo{
			Handle: hMonitor,
			Bounds: mi.Work,
		})
		return 1
	})
	procEnumDisplayMonitors.Call(0, 0, cb, 0)
	return monitors
}

// ListWindows returns a list of all open window titles and their PIDs
func ListWindows() ([]WindowInfo, error) {
	var windows []WindowInfo

	if runtime.GOOS == "windows" {
		cb := syscall.NewCallback(func(hwnd win.HWND, lparam uintptr) uintptr {
			if !win.IsWindowVisible(hwnd) {
				return 1
			}

			// Get Title
			length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
			if length == 0 {
				return 1
			}

			buf := make([]uint16, length+1)
			procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), length+1)
			title := syscall.UTF16ToString(buf)

			// Filter out common background windows
			if title == "" || title == "Settings" || title == "Microsoft Text Input Application" || title == "Program Manager" {
				return 1
			}

			// Get PID
			var pid uint32
			procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))

			windows = append(windows, WindowInfo{
				Handle:       fmt.Sprintf("%v", hwnd),
				PID:          int(pid),
				Title:        title,
				Controllable: true,
			})
			return 1
		})

		procEnumWindows.Call(cb, 0)
		return windows, nil
	}

	// Fallback for non-windows
	titles, err := robotgo.FindNames()
	if err != nil {
		return nil, err
	}

	for _, title := range titles {
		if title == "" {
			continue
		}
		pids, err := robotgo.FindIds(title)
		if err == nil && len(pids) > 0 {
			windows = append(windows, WindowInfo{
				Handle:       fmt.Sprintf("%d", pids[0]),
				PID:          pids[0],
				Title:        title,
				Controllable: true,
			})
		}
	}
	return windows, nil
}

// FocusWindow activates the window with the given handle
func FocusWindow(handle string) error {
	if runtime.GOOS == "windows" {
		var hwnd uintptr
		fmt.Sscanf(handle, "%v", &hwnd)
		if hwnd != 0 {
			win.ShowWindow(win.HWND(hwnd), win.SW_RESTORE)
			win.SetForegroundWindow(win.HWND(hwnd))
			return nil
		}
	}
	
	var pid int
	fmt.Sscanf(handle, "%d", &pid)
	return robotgo.ActivePid(pid)
}

// TypeString focuses the window and types the given string
func TypeString(handle string, text string) error {
	if err := FocusWindow(handle); err != nil {
		return err
	}
	robotgo.TypeStr(text)
	return nil
}

// GetClipboard returns the current clipboard text
func GetClipboard() (string, error) {
	return robotgo.ReadAll()
}

// SetClipboard sets the current clipboard text
func SetClipboard(text string) error {
	return robotgo.WriteAll(text)
}

// CloseWindow sends a close message to the window
func CloseWindow(handle string) error {
	if runtime.GOOS == "windows" {
		var hwnd uintptr
		fmt.Sscanf(handle, "%v", &hwnd)
		if hwnd != 0 {
			procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
		}
	}
	return nil
}

// OpenApp launches an application from a command string
func OpenApp(command string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	return cmd.Start()
}

// SetVolume adjusts system volume
func SetVolume(level int) error {
	if runtime.GOOS == "windows" {
		// powershell approach to adjust volume relative to current or absolute
		// char 174 is Vol Down, 175 is Vol Up. 
		// For simplicity, we reset to 0 and then go up.
		script := fmt.Sprintf("$obj = new-object -com wscript.shell; for($i=0; $i -lt 50; $i++) { $obj.SendKeys([char]174) }; for($i=0; $i -lt %d; $i++) { $obj.SendKeys([char]175) }", level/2)
		return exec.Command("powershell", "-Command", script).Run()
	}
	return nil
}

// ShutdownPC shuts down the computer
func ShutdownPC() error {
	if runtime.GOOS == "windows" {
		return exec.Command("shutdown", "/s", "/t", "60").Run()
	}
	return nil
}

// SnapWindow moves and resizes a window based on a position string
func SnapWindow(handle string, position string) error {
	var hwnd uintptr
	fmt.Sscanf(handle, "%v", &hwnd)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle")
	}

	if runtime.GOOS == "windows" {
		monitors := getMonitors()
		if len(monitors) == 0 {
			return fmt.Errorf("no monitors found")
		}

		// Find current monitor
		var rect win.RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		midX := (rect.Left + rect.Right) / 2
		midY := (rect.Top + rect.Bottom) / 2
		
		currentIdx := 0
		for i, m := range monitors {
			if midX >= m.Bounds.Left && midX <= m.Bounds.Right && midY >= m.Bounds.Top && midY <= m.Bounds.Bottom {
				currentIdx = i
				break
			}
		}

		if position == "next-monitor" {
			target := monitors[(currentIdx+1)%len(monitors)]
			win.ShowWindow(win.HWND(hwnd), win.SW_RESTORE)
			win.MoveWindow(win.HWND(hwnd), target.Bounds.Left, target.Bounds.Top, target.Bounds.Right-target.Bounds.Left, target.Bounds.Bottom-target.Bounds.Top, true)
			win.SetForegroundWindow(win.HWND(hwnd))
			return nil
		}

		b := monitors[currentIdx].Bounds
		sw := int(b.Right - b.Left)
		sh := int(b.Bottom - b.Top)
		ox := int(b.Left)
		oy := int(b.Top)

		var x, y, w, h int
		halfW := sw / 2
		halfH := sh / 2

		switch position {
		case "top-left":
			x, y, w, h = ox, oy, halfW, halfH
		case "top-right":
			x, y, w, h = ox+halfW, oy, halfW, halfH
		case "bottom-left":
			x, y, w, h = ox, oy+halfH, halfW, halfH
		case "bottom-right":
			x, y, w, h = ox+halfW, oy+halfH, halfW, halfH
		case "left":
			x, y, w, h = ox, oy, halfW, sh
		case "right":
			x, y, w, h = ox+halfW, oy, halfW, sh
		case "full":
			x, y, w, h = ox, oy, sw, sh
		case "center":
			x, y, w, h = ox+sw/4, oy+sh/4, halfW, halfH
		default:
			return fmt.Errorf("invalid position: %s", position)
		}

		win.ShowWindow(win.HWND(hwnd), win.SW_RESTORE)
		win.MoveWindow(win.HWND(hwnd), int32(x), int32(y), int32(w), int32(h), true)
		win.SetForegroundWindow(win.HWND(hwnd))
		return nil
	}

	// Fallback for other OSes
	var pid int
	fmt.Sscanf(handle, "%d", &pid)
	robotgo.ActivePid(pid)
	return nil
}
