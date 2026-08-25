//go:build windows && 386

package webview2

// INPUT's union starts immediately after Type on 32-bit Windows.
type mouseInput struct {
	Type      uint32
	X         int32
	Y         int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keyboardInput struct {
	Type       uint32
	VirtualKey uint16
	ScanCode   uint16
	Flags      uint32
	Time       uint32
	ExtraInfo  uintptr
	_          [8]byte
}
