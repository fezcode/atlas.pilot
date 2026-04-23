//go:build windows

package window

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/tailscale/win"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procEnumDisplayMonitors      = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetFocus                 = user32.NewProc("GetFocus")

	kernel32dll          = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThread = kernel32dll.NewProc("GetCurrentThreadId")
)

const (
	WM_CLOSE = 0x0010
	WM_CHAR  = 0x0102
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

	cb := syscall.NewCallback(func(hwnd win.HWND, lparam uintptr) uintptr {
		if !win.IsWindowVisible(hwnd) {
			return 1
		}

		length, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
		if length == 0 {
			return 1
		}

		buf := make([]uint16, length+1)
		procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), length+1)
		title := syscall.UTF16ToString(buf)

		if title == "" || title == "Settings" || title == "Microsoft Text Input Application" || title == "Program Manager" {
			return 1
		}

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

func parseHwnd(handle string) uintptr {
	var hwnd uintptr
	fmt.Sscanf(handle, "%v", &hwnd)
	return hwnd
}

// getFocusedChild returns the HWND of the control that currently has
// keyboard focus inside the given top-level window. Returns 0 on failure.
//
// GetFocus only reports focus within the caller's input queue, so we
// temporarily AttachThreadInput to the target's UI thread, query, and detach.
func getFocusedChild(topLevel uintptr) uintptr {
	var targetPID uint32
	targetTID, _, _ := procGetWindowThreadProcessId.Call(topLevel, uintptr(unsafe.Pointer(&targetPID)))
	if targetTID == 0 {
		return 0
	}
	ourTID, _, _ := procGetCurrentThread.Call()

	procAttachThreadInput.Call(ourTID, targetTID, 1)
	focused, _, _ := procGetFocus.Call()
	procAttachThreadInput.Call(ourTID, targetTID, 0)
	return focused
}

// FocusWindow activates the window with the given handle
func FocusWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	win.ShowWindow(win.HWND(hwnd), win.SW_RESTORE)
	win.SetForegroundWindow(win.HWND(hwnd))
	return nil
}

// MaximizeWindow maximizes the window with the given handle
func MaximizeWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	win.ShowWindow(win.HWND(hwnd), win.SW_MAXIMIZE)
	return nil
}

// MinimizeWindow minimizes the window with the given handle
func MinimizeWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	win.ShowWindow(win.HWND(hwnd), win.SW_MINIMIZE)
	return nil
}

// RaiseWindow brings the window to the top of the Z-order without changing
// keyboard focus or activating it — distinct from FocusWindow, which also
// steals foreground activation.
//
// Windows foreground-lock prevents a plain SetWindowPos(HWND_TOP) from a
// non-foreground process from actually rising above other apps' windows.
// The reliable workaround is the "TOPMOST pump": briefly promote to
// HWND_TOPMOST and immediately demote back to HWND_NOTOPMOST. This leaves
// the window at the top of its Z-group without permanently pinning it.
func RaiseWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	// If the window is minimized, restore it first — you can't see a raised
	// window that's hidden on the taskbar.
	if win.IsIconic(win.HWND(hwnd)) {
		win.ShowWindow(win.HWND(hwnd), win.SW_RESTORE)
	}
	const flags = win.SWP_NOMOVE | win.SWP_NOSIZE | win.SWP_NOACTIVATE | win.SWP_SHOWWINDOW
	win.SetWindowPos(win.HWND(hwnd), win.HWND_TOPMOST, 0, 0, 0, 0, flags)
	win.SetWindowPos(win.HWND(hwnd), win.HWND_NOTOPMOST, 0, 0, 0, 0, flags)
	return nil
}

// LowerWindow sends the window to the bottom of the Z-order (behind every
// other top-level window) without deactivating or minimizing it.
func LowerWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	const flags = win.SWP_NOMOVE | win.SWP_NOSIZE | win.SWP_NOACTIVATE
	win.SetWindowPos(win.HWND(hwnd), win.HWND_BOTTOM, 0, 0, 0, 0, flags)
	return nil
}

// TypeString focuses the window and delivers the given string via WM_CHAR
// messages posted to the focused child. WM_CHAR bypasses the keyboard queue
// and is both faster and more reliable than SendInput for text injection —
// SendInput+KEYEVENTF_UNICODE gets mangled under IMM-aware apps like Notepad
// (characters drop, scancodes latch, etc).
func TypeString(handle string, text string) error {
	if err := FocusWindow(handle); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)

	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}

	// Walk to the focused descendant (e.g. Notepad's Edit control) if we can;
	// WM_CHAR must go to the window that actually has keyboard focus.
	target := getFocusedChild(hwnd)
	if target == 0 {
		target = hwnd
	}

	for _, u := range utf16.Encode([]rune(text)) {
		procPostMessageW.Call(target, WM_CHAR, uintptr(u), 0)
	}
	return nil
}

// SendKey focuses the window and taps a special key
func SendKey(handle string, key string) error {
	if handle != "" {
		if err := FocusWindow(handle); err != nil {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
	return tapKey(key)
}

// SendHotkey focuses the window and taps a key with modifiers
func SendHotkey(handle string, key string, modifiers []string) error {
	if handle != "" {
		if err := FocusWindow(handle); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return tapHotkey(key, modifiers)
}

// MoveMouseRelative moves the mouse cursor by dx, dy using SendInput, which
// generates proper WM_MOUSEMOVE events and works in contexts where
// SetCursorPos alone is silently blocked (elevated targets, RDP, etc).
func MoveMouseRelative(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	_ = mouseMoveRelative(int32(dx), int32(dy))
}

// ClickMouse performs a mouse click
func ClickMouse(button string, double bool) {
	_ = mouseClick(button, double)
}

// ScrollMouse scrolls the mouse wheel
func ScrollMouse(x, y int) {
	_ = mouseScroll(x, y)
}

// GetClipboard returns the current clipboard text
func GetClipboard() (string, error) {
	return clipboardGetText()
}

// SetClipboard sets the current clipboard text
func SetClipboard(text string) error {
	return clipboardSetText(text)
}

// PasteIntoWindow focuses the window and sends a paste command
func PasteIntoWindow(handle string) error {
	if err := FocusWindow(handle); err != nil {
		return err
	}
	time.Sleep(50 * time.Millisecond)
	return tapHotkey("v", []string{"control"})
}

// CaptureWindow takes a screenshot of the specified window and returns it as PNG bytes
func CaptureWindow(handle string) ([]byte, error) {
	if err := FocusWindow(handle); err != nil {
		return nil, err
	}
	time.Sleep(200 * time.Millisecond)

	hwnd := parseHwnd(handle)
	var rect win.RECT
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))

	w := int(rect.Right - rect.Left)
	h := int(rect.Bottom - rect.Top)

	return captureRegionPNG(int(rect.Left), int(rect.Top), w, h)
}

// CloseWindow sends a close message to the window
func CloseWindow(handle string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle: %q", handle)
	}
	procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	return nil
}

// OpenApp launches an application from a command string
func OpenApp(command string) error {
	cmd := exec.Command("cmd", "/C", command)
	return cmd.Start()
}

// SetVolume adjusts system volume
func SetVolume(level int) error {
	// Reset to 0 then step up. char 174 = VolDown, 175 = VolUp.
	script := fmt.Sprintf(
		"$obj = new-object -com wscript.shell; for($i=0; $i -lt 50; $i++) { $obj.SendKeys([char]174) }; for($i=0; $i -lt %d; $i++) { $obj.SendKeys([char]175) }",
		level/2,
	)
	return exec.Command("powershell", "-Command", script).Run()
}

// ShutdownPC shuts down the computer
func ShutdownPC() error {
	return exec.Command("shutdown", "/s", "/t", "60").Run()
}

// RestartPC restarts the computer
func RestartPC() error {
	return exec.Command("shutdown", "/r", "/t", "60").Run()
}

// SleepPC puts the computer to sleep
func SleepPC() error {
	return exec.Command("rundll32.exe", "powprof.dll,SetSuspendState", "0,1,0").Run()
}

// LockPC locks the computer
func LockPC() error {
	return exec.Command("rundll32.exe", "user32.dll,LockWorkStation").Run()
}

// SnapWindow moves and resizes a window based on a position string
func SnapWindow(handle string, position string) error {
	hwnd := parseHwnd(handle)
	if hwnd == 0 {
		return fmt.Errorf("invalid handle")
	}

	monitors := getMonitors()
	if len(monitors) == 0 {
		return fmt.Errorf("no monitors found")
	}

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
