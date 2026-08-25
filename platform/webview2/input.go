package webview2

// MouseButton identifies a button for trusted native input.
type MouseButton uint8

const (
	MouseButtonLeft MouseButton = iota + 1
	MouseButtonRight
	MouseButtonMiddle
)

// MouseAction is an explicit native mouse operation. It is separate from the
// fast in-page synthetic interaction API because only this path produces input
// forwarded by Windows to WebView2.
type MouseAction struct {
	X      int
	Y      int
	Button MouseButton
	Clicks int
}

// KeyAction is an explicit native keyboard operation. Text uses Unicode input;
// Key uses a Windows virtual-key code when Text is empty.
type KeyAction struct {
	Text      string
	Key       uint16
	Modifiers KeyModifiers
}

type KeyModifiers uint8

const (
	KeyModifierShift KeyModifiers = 1 << iota
	KeyModifierControl
	KeyModifierAlt
	KeyModifierMeta
)
