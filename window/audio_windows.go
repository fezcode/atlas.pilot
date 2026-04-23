//go:build windows

package window

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Core Audio COM bindings — just enough to read the current master
// playback volume via IAudioEndpointVolume::GetMasterVolumeLevelScalar.
//
// We read (pointer arg) but don't write through COM because writing
// requires SetMasterVolumeLevelScalar(float, GUID*) — the float arg goes
// in XMM1 on Windows x64, and Go's syscall package only populates
// integer registers. That's a known limitation; the legacy PowerShell
// SetVolume path still handles writes.

var (
	ole32dll             = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32dll.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32dll.NewProc("CoUninitialize")
	procCoCreateInstance = ole32dll.NewProc("CoCreateInstance")
)

const (
	coinitApartmentThreaded = 2
	clsctxAll               = 23
)

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidMMDeviceEnumerator = winGUID{0xBCDE0395, 0xE52F, 0x467C,
		[8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator = winGUID{0xA95664D2, 0x9614, 0x4F35,
		[8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioEndpointVolume = winGUID{0x5CDF2C82, 0x841E, 0x4546,
		[8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

// comCall invokes COM vtable slot `slot` on interface `iface`.
// `slot` is the absolute vtable index (IUnknown occupies slots 0..2).
func comCall(iface uintptr, slot uintptr, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(iface))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + slot*unsafe.Sizeof(uintptr(0))))
	full := append([]uintptr{iface}, args...)
	ret, _, _ := syscall.SyscallN(fn, full...)
	return ret
}

func comRelease(iface uintptr) {
	if iface != 0 {
		comCall(iface, 2) // IUnknown::Release
	}
}

// GetSystemVolume returns the current master output volume as an integer
// percentage in the range 0..100.
func GetSystemVolume() (int, error) {
	// Pin this goroutine to its OS thread — COM apartment state is per-thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded)
	// S_OK = 0 means we initialized; S_FALSE = 1 means already initialized
	// on this thread; RPC_E_CHANGED_MODE = 0x80010106 means another apartment
	// mode is active (still usable). Only pair Uninitialize with S_OK.
	if hr == 0 {
		defer procCoUninitialize.Call()
	}

	var enumerator uintptr
	rc, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if rc != 0 || enumerator == 0 {
		return 0, fmt.Errorf("CoCreateInstance(MMDeviceEnumerator): 0x%x", rc)
	}
	defer comRelease(enumerator)

	// IMMDeviceEnumerator::GetDefaultAudioEndpoint — vtable slot 4.
	// Signature: HRESULT(EDataFlow flow, ERole role, IMMDevice** out)
	// eRender=0, eConsole=0.
	var device uintptr
	if rc := comCall(enumerator, 4, 0, 0, uintptr(unsafe.Pointer(&device))); rc != 0 || device == 0 {
		return 0, fmt.Errorf("GetDefaultAudioEndpoint: 0x%x", rc)
	}
	defer comRelease(device)

	// IMMDevice::Activate — vtable slot 3.
	// Signature: HRESULT(REFIID iid, DWORD clsctx, PROPVARIANT* params, void** out)
	var endpoint uintptr
	if rc := comCall(device, 3,
		uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)),
		clsctxAll, 0,
		uintptr(unsafe.Pointer(&endpoint)),
	); rc != 0 || endpoint == 0 {
		return 0, fmt.Errorf("IMMDevice::Activate(IAudioEndpointVolume): 0x%x", rc)
	}
	defer comRelease(endpoint)

	// IAudioEndpointVolume::GetMasterVolumeLevelScalar — vtable slot 9.
	// Signature: HRESULT(float* pfLevel) — writes 0.0..1.0 into *pfLevel.
	var level float32
	if rc := comCall(endpoint, 9, uintptr(unsafe.Pointer(&level))); rc != 0 {
		return 0, fmt.Errorf("GetMasterVolumeLevelScalar: 0x%x", rc)
	}

	pct := int(level*100 + 0.5)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, nil
}
