//go:build linux && !rush_wpe

package rush

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ebitengine/purego"
)

type x11Input struct {
	mu      sync.Mutex
	display uintptr
	x11     uintptr
	xtst    uintptr

	closeDisplay    func(uintptr) int32
	flush           func(uintptr) int32
	sync            func(uintptr, int32) int32
	stringToKeysym  func(*byte) uintptr
	keysymToKeycode func(uintptr, uintptr) uint8
	fakeMotion      func(uintptr, int32, int32, int32, uint64) int32
	fakeButton      func(uintptr, uint32, int32, uint64) int32
	fakeKey         func(uintptr, uint32, int32, uint64) int32
}

func newNativeInput() (nativeInput, error) {
	displayName := os.Getenv("DISPLAY")
	if displayName == "" {
		return nil, errors.New("trusted native input requires an X11 DISPLAY")
	}
	x11, err := purego.Dlopen("libX11.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load X11 for trusted input: %w", err)
	}
	xtst, err := purego.Dlopen("libXtst.so.6", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		_ = purego.Dlclose(x11)
		return nil, fmt.Errorf("load XTest for trusted input: %w", err)
	}
	driver := &x11Input{x11: x11, xtst: xtst}
	purego.RegisterLibFunc(&driver.closeDisplay, x11, "XCloseDisplay")
	purego.RegisterLibFunc(&driver.flush, x11, "XFlush")
	purego.RegisterLibFunc(&driver.sync, x11, "XSync")
	purego.RegisterLibFunc(&driver.stringToKeysym, x11, "XStringToKeysym")
	purego.RegisterLibFunc(&driver.keysymToKeycode, x11, "XKeysymToKeycode")
	purego.RegisterLibFunc(&driver.fakeMotion, xtst, "XTestFakeMotionEvent")
	purego.RegisterLibFunc(&driver.fakeButton, xtst, "XTestFakeButtonEvent")
	purego.RegisterLibFunc(&driver.fakeKey, xtst, "XTestFakeKeyEvent")
	name := append([]byte(displayName), 0)
	var openDisplay func(*byte) uintptr
	purego.RegisterLibFunc(&openDisplay, x11, "XOpenDisplay")
	driver.display = openDisplay(&name[0])
	if driver.display == 0 {
		_ = purego.Dlclose(xtst)
		_ = purego.Dlclose(x11)
		return nil, fmt.Errorf("open X11 display %q for trusted input", displayName)
	}
	return driver, nil
}

func (driver *x11Input) Do(request NativeInputRequest) error {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if driver.display == 0 {
		return errors.New("trusted native input is closed")
	}
	switch request.Action {
	case "click":
		if err := driver.moveAndClick(request.X, request.Y); err != nil {
			return err
		}
	case "type":
		if err := driver.moveAndClick(request.X, request.Y); err != nil {
			return err
		}
		driver.flush(driver.display)
		time.Sleep(10 * time.Millisecond)
		for _, character := range request.Text {
			if err := driver.typeRune(character); err != nil {
				return err
			}
			driver.flush(driver.display)
			time.Sleep(5 * time.Millisecond)
		}
	case "press":
		if request.X != 0 || request.Y != 0 {
			if err := driver.moveAndClick(request.X, request.Y); err != nil {
				return err
			}
		}
		if err := driver.pressChord(request.Key); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown native input action %q", request.Action)
	}
	driver.flush(driver.display)
	return nil
}

func (driver *x11Input) moveAndClick(x, y float64) error {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return errors.New("native input target has invalid coordinates")
	}
	if driver.fakeMotion(driver.display, -1, int32(math.Round(x)), int32(math.Round(y)), 0) == 0 ||
		driver.sync(driver.display, 0) == 0 {
		return errors.New("XTest rejected trusted mouse motion")
	}
	// Let WebKit process the crossing and motion events before the button press.
	// This matters for controls whose hover/focus state changes their layout.
	time.Sleep(5 * time.Millisecond)
	if driver.fakeButton(driver.display, 1, 1, 0) == 0 || driver.fakeButton(driver.display, 1, 0, 0) == 0 ||
		driver.sync(driver.display, 0) == 0 {
		return errors.New("XTest rejected trusted mouse input")
	}
	return nil
}

func (driver *x11Input) pressChord(chord string) error {
	parts := strings.Split(chord, "+")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return errors.New("native key must not be empty")
	}
	modifiers := make([]uint32, 0, len(parts)-1)
	for _, modifier := range parts[:len(parts)-1] {
		name, ok := x11KeyNames[strings.ToLower(modifier)]
		if !ok || !strings.HasSuffix(name, "_L") {
			return fmt.Errorf("unsupported native key modifier %q", modifier)
		}
		code, err := driver.keycode(name)
		if err != nil {
			return err
		}
		modifiers = append(modifiers, code)
		driver.fakeKey(driver.display, code, 1, 0)
	}
	keyName := parts[len(parts)-1]
	if mapped, ok := x11KeyNames[strings.ToLower(keyName)]; ok {
		keyName = mapped
	}
	code, err := driver.keycode(keyName)
	if err != nil {
		return err
	}
	driver.fakeKey(driver.display, code, 1, 0)
	driver.fakeKey(driver.display, code, 0, 0)
	for index := len(modifiers) - 1; index >= 0; index-- {
		driver.fakeKey(driver.display, modifiers[index], 0, 0)
	}
	return nil
}

func (driver *x11Input) typeRune(character rune) error {
	name, shift := x11RuneKey(character)
	code, err := driver.keycode(name)
	if err != nil && character > unicode.MaxASCII {
		keysym := uintptr(0x01000000 | character)
		code = uint32(driver.keysymToKeycode(driver.display, keysym))
		err = nil
		if code == 0 {
			err = fmt.Errorf("current X11 keyboard layout cannot type %q", character)
		}
	}
	if err != nil {
		return err
	}
	var shiftCode uint32
	if shift {
		shiftCode, err = driver.keycode("Shift_L")
		if err != nil {
			return err
		}
		driver.fakeKey(driver.display, shiftCode, 1, 0)
	}
	driver.fakeKey(driver.display, code, 1, 0)
	driver.fakeKey(driver.display, code, 0, 0)
	if shift {
		driver.fakeKey(driver.display, shiftCode, 0, 0)
	}
	return nil
}

func x11RuneKey(character rune) (string, bool) {
	if name, ok := x11RuneNames[character]; ok {
		return name, strings.ContainsRune("~!@#$%^&*()_+{}|:\"<>?", character)
	}
	return string(character), unicode.IsUpper(character)
}

func (driver *x11Input) keycode(name string) (uint32, error) {
	terminated := append([]byte(name), 0)
	keysym := driver.stringToKeysym(&terminated[0])
	if keysym == 0 {
		return 0, fmt.Errorf("unknown native key %q", name)
	}
	code := uint32(driver.keysymToKeycode(driver.display, keysym))
	if code == 0 {
		return 0, fmt.Errorf("current X11 keyboard layout has no key for %q", name)
	}
	return code, nil
}

func (driver *x11Input) Close() error {
	driver.mu.Lock()
	defer driver.mu.Unlock()
	if driver.display != 0 {
		driver.closeDisplay(driver.display)
		driver.display = 0
	}
	if driver.xtst != 0 {
		_ = purego.Dlclose(driver.xtst)
		driver.xtst = 0
	}
	if driver.x11 != 0 {
		_ = purego.Dlclose(driver.x11)
		driver.x11 = 0
	}
	return nil
}

var x11KeyNames = map[string]string{
	"alt": "Alt_L", "backspace": "BackSpace", "control": "Control_L", "ctrl": "Control_L",
	"delete": "Delete", "down": "Down", "end": "End", "enter": "Return", "escape": "Escape",
	"home": "Home", "left": "Left", "meta": "Super_L", "pagedown": "Page_Down", "pageup": "Page_Up",
	"right": "Right", "shift": "Shift_L", "space": "space", "tab": "Tab", "up": "Up",
}

var x11RuneNames = map[rune]string{
	' ': "space", '!': "exclam", '"': "quotedbl", '#': "numbersign", '$': "dollar",
	'%': "percent", '&': "ampersand", '\'': "apostrophe", '(': "parenleft", ')': "parenright",
	'*': "asterisk", '+': "plus", ',': "comma", '-': "minus", '.': "period", '/': "slash",
	':': "colon", ';': "semicolon", '<': "less", '=': "equal", '>': "greater", '?': "question",
	'@': "at", '[': "bracketleft", '\\': "backslash", ']': "bracketright", '^': "asciicircum",
	'_': "underscore", '`': "grave", '{': "braceleft", '|': "bar", '}': "braceright", '~': "asciitilde",
}
