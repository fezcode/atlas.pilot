//go:build windows

package window

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/tailscale/win"
)

var (
	// user32 is declared in manager.go; reuse it via procs.
	procSendInput     = user32.NewProc("SendInput")
	procOpenClipboard = user32.NewProc("OpenClipboard")
	procCloseClipboard  = user32.NewProc("CloseClipboard")
	procEmptyClipboard  = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")

	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
)

// SendInput input types
const (
	inputMouse    = 0
	inputKeyboard = 1

	keyeventfKeyup = 0x0002

	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfWheel      = 0x0800
	mouseeventfHWheel     = 0x01000

	wheelDelta = 120

	// Clipboard
	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	// GetDIBits
	dibRGBColors = 0
	biRGB        = 0

	// BitBlt
	srcCopy = 0x00CC0020
)

// rawInput mirrors the Win32 INPUT union. On x64 SendInput expects cbSize=40.
// Layout: uint32 type + 4 bytes padding (ULONG_PTR alignment) + 32-byte union.
type rawInput struct {
	Type  uint32
	_     uint32
	Union [32]byte
}

type mouseInputData struct {
	Dx, Dy      int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type kbdInputData struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

func sendInputs(inputs []rawInput) error {
	if len(inputs) == 0 {
		return nil
	}
	ret, _, err := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(rawInput{}),
	)
	if int(ret) != len(inputs) {
		return fmt.Errorf("SendInput injected %d of %d events: %v", ret, len(inputs), err)
	}
	return nil
}

// vkMap translates the high-level key names used by the frontend to Windows
// virtual-key codes. Names are case-insensitive.
var vkMap = map[string]uint16{
	// modifiers
	"control": 0x11, "ctrl": 0x11,
	"shift":   0x10,
	"alt":     0x12,
	"win":     0x5B, "cmd": 0x5B, "super": 0x5B,

	// navigation / editing
	"backspace": 0x08,
	"tab":       0x09,
	"enter":     0x0D, "return": 0x0D,
	"escape": 0x1B, "esc": 0x1B,
	"space":    0x20,
	"pageup":   0x21,
	"pagedown": 0x22,
	"end":      0x23,
	"home":     0x24,
	"left":     0x25,
	"up":       0x26,
	"right":    0x27,
	"down":     0x28,
	"insert":   0x2D,
	"delete":   0x2E, "del": 0x2E,

	// function
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73,
	"f5": 0x74, "f6": 0x75, "f7": 0x76, "f8": 0x77,
	"f9": 0x78, "f10": 0x79, "f11": 0x7A, "f12": 0x7B,
}

// resolveVK maps a user-provided key name to a virtual-key code.
// Single printable characters fall back to VkKeyScan / the letter/digit VK.
func resolveVK(name string) (uint16, bool) {
	if name == "" {
		return 0, false
	}
	lower := strings.ToLower(name)
	if vk, ok := vkMap[lower]; ok {
		return vk, true
	}
	// Single character: letters A-Z map to 0x41..0x5A, digits 0-9 to 0x30..0x39.
	if len(lower) == 1 {
		c := lower[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint16(c - 'a' + 0x41), true
		case c >= '0' && c <= '9':
			return uint16(c - '0' + 0x30), true
		}
	}
	return 0, false
}

func makeKbdInput(vk uint16, flags uint32) rawInput {
	var ri rawInput
	ri.Type = inputKeyboard
	kb := (*kbdInputData)(unsafe.Pointer(&ri.Union[0]))
	kb.WVk = vk
	kb.DwFlags = flags
	return ri
}

func makeMouseInput(dx, dy int32, mouseData, flags uint32) rawInput {
	var ri rawInput
	ri.Type = inputMouse
	mi := (*mouseInputData)(unsafe.Pointer(&ri.Union[0]))
	mi.Dx = dx
	mi.Dy = dy
	mi.MouseData = mouseData
	mi.DwFlags = flags
	return ri
}

// tapKey presses and releases a single named key.
func tapKey(name string) error {
	vk, ok := resolveVK(name)
	if !ok {
		return fmt.Errorf("unknown key: %q", name)
	}
	return sendInputs([]rawInput{
		makeKbdInput(vk, 0),
		makeKbdInput(vk, keyeventfKeyup),
	})
}

// tapHotkey presses the named modifiers, taps the key, then releases modifiers.
func tapHotkey(key string, modifiers []string) error {
	vk, ok := resolveVK(key)
	if !ok {
		return fmt.Errorf("unknown key: %q", key)
	}
	modVKs := make([]uint16, 0, len(modifiers))
	for _, m := range modifiers {
		mv, mok := resolveVK(m)
		if !mok {
			return fmt.Errorf("unknown modifier: %q", m)
		}
		modVKs = append(modVKs, mv)
	}

	inputs := make([]rawInput, 0, len(modVKs)*2+2)
	for _, mv := range modVKs {
		inputs = append(inputs, makeKbdInput(mv, 0))
	}
	inputs = append(inputs, makeKbdInput(vk, 0), makeKbdInput(vk, keyeventfKeyup))
	for i := len(modVKs) - 1; i >= 0; i-- {
		inputs = append(inputs, makeKbdInput(modVKs[i], keyeventfKeyup))
	}
	return sendInputs(inputs)
}

// mouseMoveRelative injects a relative mouse-move event.
func mouseMoveRelative(dx, dy int32) error {
	return sendInputs([]rawInput{
		makeMouseInput(dx, dy, 0, mouseeventfMove),
	})
}

func mouseClick(button string, double bool) error {
	var down, up uint32
	switch strings.ToLower(button) {
	case "", "left":
		down, up = mouseeventfLeftDown, mouseeventfLeftUp
	case "right":
		down, up = mouseeventfRightDown, mouseeventfRightUp
	case "middle", "center":
		down, up = mouseeventfMiddleDown, mouseeventfMiddleUp
	default:
		return fmt.Errorf("unknown mouse button: %q", button)
	}
	events := []rawInput{
		makeMouseInput(0, 0, 0, down),
		makeMouseInput(0, 0, 0, up),
	}
	if double {
		events = append(events,
			makeMouseInput(0, 0, 0, down),
			makeMouseInput(0, 0, 0, up),
		)
	}
	return sendInputs(events)
}

// mouseScroll emits wheel events. y>0 scrolls up, y<0 scrolls down; x<0 left, x>0 right.
func mouseScroll(x, y int) error {
	var events []rawInput
	if y != 0 {
		events = append(events, makeMouseInput(0, 0, uint32(int32(y)*wheelDelta), mouseeventfWheel))
	}
	if x != 0 {
		events = append(events, makeMouseInput(0, 0, uint32(int32(x)*wheelDelta), mouseeventfHWheel))
	}
	return sendInputs(events)
}

// --- Clipboard ----------------------------------------------------------------

func clipboardOpen() error {
	// Retry briefly: the clipboard can be momentarily held by other processes.
	var lastErr error
	for i := 0; i < 10; i++ {
		ret, _, err := procOpenClipboard.Call(0)
		if ret != 0 {
			return nil
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("OpenClipboard failed after retries: %v", lastErr)
}

func clipboardClose() {
	procCloseClipboard.Call()
}

func clipboardGetText() (string, error) {
	if err := clipboardOpen(); err != nil {
		return "", err
	}
	defer clipboardClose()

	h, _, err := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", fmt.Errorf("GetClipboardData(CF_UNICODETEXT): %v", err)
	}
	// GlobalLock returns unsafe.Pointer directly (kernel-managed memory, not GC heap).
	base := win.GlobalLock(win.HGLOBAL(h))
	if base == nil {
		return "", fmt.Errorf("GlobalLock returned nil")
	}
	defer win.GlobalUnlock(win.HGLOBAL(h))

	var chars []uint16
	for i := 0; i < (1 << 24); i++ { // 16M-char sanity cap
		u := *(*uint16)(unsafe.Add(base, i*2))
		if u == 0 {
			break
		}
		chars = append(chars, u)
	}
	return string(utf16.Decode(chars)), nil
}

func clipboardSetText(s string) error {
	if err := clipboardOpen(); err != nil {
		return err
	}
	defer clipboardClose()

	procEmptyClipboard.Call()

	units := utf16.Encode([]rune(s))
	bytesNeeded := uintptr(len(units)+1) * 2 // room for NUL terminator

	hMem := win.GlobalAlloc(gmemMoveable, bytesNeeded)
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}

	base := win.GlobalLock(hMem)
	if base == nil {
		win.GlobalFree(hMem)
		return fmt.Errorf("GlobalLock returned nil")
	}

	for i, u := range units {
		*(*uint16)(unsafe.Add(base, i*2)) = u
	}
	*(*uint16)(unsafe.Add(base, len(units)*2)) = 0

	win.GlobalUnlock(hMem)

	ret, _, err := procSetClipboardData.Call(cfUnicodeText, uintptr(hMem))
	if ret == 0 {
		win.GlobalFree(hMem)
		return fmt.Errorf("SetClipboardData: %v", err)
	}
	// Ownership of hMem transfers to the system on success.
	return nil
}

// --- Screenshot ---------------------------------------------------------------

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32 // unused for 32-bit, but BITMAPINFO requires the field
}

// captureRegionPNG captures the screen region (x,y,w,h) and encodes it as PNG.
func captureRegionPNG(x, y, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("invalid capture dimensions: %dx%d", w, h)
	}

	screenDC, _, err := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC: %v", err)
	}
	defer procReleaseDC.Call(0, screenDC)

	memDC, _, err := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %v", err)
	}
	defer procDeleteDC.Call(memDC)

	bmp, _, err := procCreateCompatibleBmp.Call(screenDC, uintptr(w), uintptr(h))
	if bmp == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap: %v", err)
	}
	defer procDeleteObject.Call(bmp)

	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)

	ret, _, err := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, uintptr(x), uintptr(y), srcCopy)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt: %v", err)
	}

	// Extract pixels as 32-bit BGRA, top-down (negative height).
	stride := w * 4
	pixels := make([]byte, stride*h)

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h) // top-down
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB

	ret, _, err = procGetDIBits.Call(
		memDC,
		bmp,
		0,
		uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bi)),
		dibRGBColors,
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits: %v", err)
	}

	// Convert BGRA → RGBA in place.
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		src := row * stride
		dst := row * img.Stride
		for col := 0; col < w; col++ {
			b := pixels[src+col*4+0]
			g := pixels[src+col*4+1]
			r := pixels[src+col*4+2]
			a := pixels[src+col*4+3]
			if a == 0 {
				// BitBlt from screen doesn't populate alpha; force opaque.
				a = 0xFF
			}
			img.Pix[dst+col*4+0] = r
			img.Pix[dst+col*4+1] = g
			img.Pix[dst+col*4+2] = b
			img.Pix[dst+col*4+3] = a
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("png encode: %w", err)
	}
	return out.Bytes(), nil
}
