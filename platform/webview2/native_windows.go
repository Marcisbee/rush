//go:build windows

package webview2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

const (
	wmDestroy      = 0x0002
	wmSize         = 0x0005
	wmClose        = 0x0010
	wmApp          = 0x8000
	wmRushDispatch = wmApp + 0x52

	wsOverlappedWindow = 0x00CF0000
	cwUseDefault       = ^uint32(0x7fffffff)
	swHide             = 0
	swShow             = 5
	swpNoSize          = 0x0001
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010

	coinitApartmentThreaded = 0x2
	keyeventfKeyUp          = 0x0002
	keyeventfUnicode        = 0x0004
	inputMouse              = 0
	inputKeyboard           = 1
	mouseeventfMove         = 0x0001
	mouseeventfLeftDown     = 0x0002
	mouseeventfLeftUp       = 0x0004
	mouseeventfRightDown    = 0x0008
	mouseeventfRightUp      = 0x0010
	mouseeventfMiddleDown   = 0x0020
	mouseeventfMiddleUp     = 0x0040
	mouseeventfAbsolute     = 0x8000
	mouseeventfVirtualDesk  = 0x4000

	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	ole32                   = windows.NewLazySystemDLL("ole32.dll")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procUpdateWindow        = user32.NewProc("UpdateWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadID   = user32.NewProc("GetWindowThreadProcessId")
	procAttachThreadInput   = user32.NewProc("AttachThreadInput")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procSetActiveWindow     = user32.NewProc("SetActiveWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetFocus            = user32.NewProc("SetFocus")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procSendInput           = user32.NewProc("SendInput")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadID  = kernel32.NewProc("GetCurrentThreadId")
	procGlobalLock          = kernel32.NewProc("GlobalLock")
	procGlobalUnlock        = kernel32.NewProc("GlobalUnlock")
	procGlobalSize          = kernel32.NewProc("GlobalSize")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCreateStreamGlobal  = ole32.NewProc("CreateStreamOnHGlobal")
	procGetHGlobalStream    = ole32.NewProc("GetHGlobalFromStream")

	windowClassOnce sync.Once
	windowClassErr  error
	windowClassName = windows.StringToUTF16Ptr("RushWebView2Host")
	windowsByHandle sync.Map
)

type windowsDriver struct {
	config    Config
	realmID   string
	hwnd      uintptr
	chromium  *edge.Chromium
	calls     chan uiCall
	done      chan struct{}
	started   chan error
	onMessage func(json.RawMessage)

	navMu   sync.Mutex
	navDone chan struct{}

	pendingMu sync.Mutex
	pending   map[uint64]chan evaluation
	nextID    atomic.Uint64
	closed    atomic.Bool
}

type uiCall struct {
	fn   func() error
	done chan error
}

type evaluation struct {
	value json.RawMessage
	err   error
}

type internalEnvelope struct {
	Kind  string          `json:"__rush"`
	ID    uint64          `json:"id"`
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error string          `json:"error"`
}

type winPoint struct{ X, Y int32 }
type winMessage struct {
	HWND    uintptr
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   winPoint
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}

type chromiumLayout struct {
	hwnd       uintptr
	padding    struct{ Left, Top, Right, Bottom int32 }
	controller *edge.ICoreWebView2Controller
	webview    *edge.ICoreWebView2
}

// These minimal COM layouts intentionally cover only WebView2's initial
// ICoreWebView2 and IStream methods. Keeping the capture seam local avoids
// importing generated callback wrappers for hundreds of unrelated interfaces.
type rawCoreWebView struct{ vtbl *rawCoreWebViewVtbl }
type rawCoreWebViewVtbl struct {
	QueryInterface, AddRef, Release                             uintptr
	GetSettings, GetSource, Navigate, NavigateToString          uintptr
	AddNavigationStarting, RemoveNavigationStarting             uintptr
	AddContentLoading, RemoveContentLoading                     uintptr
	AddSourceChanged, RemoveSourceChanged                       uintptr
	AddHistoryChanged, RemoveHistoryChanged                     uintptr
	AddNavigationCompleted, RemoveNavigationCompleted           uintptr
	AddFrameNavigationStarting, RemoveFrameNavigationStarting   uintptr
	AddFrameNavigationCompleted, RemoveFrameNavigationCompleted uintptr
	AddScriptDialogOpening, RemoveScriptDialogOpening           uintptr
	AddPermissionRequested, RemovePermissionRequested           uintptr
	AddProcessFailed, RemoveProcessFailed                       uintptr
	AddScriptToExecuteOnDocumentCreated                         uintptr
	RemoveScriptToExecuteOnDocumentCreated                      uintptr
	ExecuteScript, CapturePreview                               uintptr
}

type rawStream struct{ vtbl *rawStreamVtbl }
type rawStreamVtbl struct{ QueryInterface, AddRef, Release, Read, Write uintptr }

func (s *rawStream) release() { syscall.SyscallN(s.vtbl.Release, uintptr(unsafe.Pointer(s))) }

func newPlatformDriver() nativeDriver {
	return &windowsDriver{calls: make(chan uiCall, 128), done: make(chan struct{}), started: make(chan error, 1), pending: make(map[uint64]chan evaluation)}
}

func (d *windowsDriver) start(ctx context.Context, config Config, realmID string, onMessage func(json.RawMessage)) error {
	d.config, d.realmID, d.onMessage = config, realmID, onMessage
	go d.runUIThread()
	select {
	case err := <-d.started:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.navigate(ctx, resetDocument)
}

func (d *windowsDriver) runUIThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(d.done)
	if hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); int32(hr) < 0 {
		d.started <- fmt.Errorf("webview2: CoInitializeEx failed: %w", syscall.Errno(hr))
		return
	}
	defer procCoUninitialize.Call()
	if err := registerWindowClass(); err != nil {
		d.started <- err
		return
	}

	title := windows.StringToUTF16Ptr("Rush WebView2 - " + d.realmID)
	windowX, windowY := uintptr(cwUseDefault), uintptr(cwUseDefault)
	if d.config.Mode == ModeHidden {
		windowX, windowY = winCoord(-32000), winCoord(-32000)
	}
	hwnd, _, createErr := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(windowClassName)), uintptr(unsafe.Pointer(title)), wsOverlappedWindow,
		windowX, windowY, uintptr(d.config.Width), uintptr(d.config.Height), 0, 0, 0, 0)
	if hwnd == 0 {
		d.started <- fmt.Errorf("webview2: CreateWindowExW failed: %w", createErr)
		return
	}
	d.hwnd = hwnd
	windowsByHandle.Store(hwnd, d)
	defer windowsByHandle.Delete(hwnd)

	chromium := edge.NewChromium()
	d.chromium = chromium
	chromium.DataPath = d.config.UserDataDir + "\\" + d.realmID
	chromium.BrowserPath = d.config.BrowserExecutableFolder
	chromium.Debug = d.config.Mode == ModeDebug
	chromium.SetErrorCallback(func(err error) {
		select {
		case d.started <- fmt.Errorf("webview2: %w", err):
		default:
		}
	})
	chromium.MessageCallback = func(message string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		d.message(message)
	}
	chromium.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		d.navMu.Lock()
		wait := d.navDone
		d.navDone = nil
		d.navMu.Unlock()
		if wait != nil {
			close(wait)
		}
	}

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				select {
				case d.started <- fmt.Errorf("webview2: initialization panic: %v", recovered):
				default:
				}
			}
		}()
		chromium.Embed(hwnd)
	}()
	if d.closed.Load() {
		procDestroyWindow.Call(hwnd)
		return
	}
	chromium.Resize()
	if d.config.Mode == ModeDebug {
		procShowWindow.Call(hwnd, swShow)
		procUpdateWindow.Call(hwnd)
		_ = chromium.Show()
		chromium.OpenDevToolsWindow()
	} else {
		_ = chromium.Hide()
		procShowWindow.Call(hwnd, swHide)
	}
	select {
	case d.started <- nil:
	default:
	}

	var message winMessage
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&message)))
	}
}

func registerWindowClass() error {
	windowClassOnce.Do(func() {
		instance, _, err := procGetModuleHandleW.Call(0)
		if instance == 0 {
			windowClassErr = fmt.Errorf("webview2: GetModuleHandleW failed: %w", err)
			return
		}
		class := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: windows.NewCallback(rushWindowProc), Instance: instance, ClassName: windowClassName}
		if atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			windowClassErr = fmt.Errorf("webview2: RegisterClassExW failed: %w", callErr)
		}
	})
	return windowClassErr
}

func rushWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	value, exists := windowsByHandle.Load(hwnd)
	if exists {
		driver := value.(*windowsDriver)
		switch message {
		case wmRushDispatch:
			for {
				select {
				case call := <-driver.calls:
					call.done <- call.fn()
				default:
					return 0
				}
			}
		case wmSize:
			if driver.chromium != nil {
				driver.chromium.Resize()
			}
			return 0
		case wmClose:
			procDestroyWindow.Call(hwnd)
			return 0
		case wmDestroy:
			procPostQuitMessage.Call(0)
			return 0
		}
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (d *windowsDriver) dispatch(ctx context.Context, fn func() error) error {
	if d.closed.Load() {
		return errors.New("webview2: native realm is closed")
	}
	call := uiCall{fn: fn, done: make(chan error, 1)}
	select {
	case d.calls <- call:
	case <-ctx.Done():
		return ctx.Err()
	case <-d.done:
		return errors.New("webview2: native host stopped")
	}
	if result, _, err := procPostMessageW.Call(d.hwnd, wmRushDispatch, 0, 0); result == 0 {
		return fmt.Errorf("webview2: PostMessageW failed: %w", err)
	}
	select {
	case err := <-call.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-d.done:
		return errors.New("webview2: native host stopped")
	}
}

func (d *windowsDriver) navigate(ctx context.Context, html string) error {
	wait := make(chan struct{})
	d.navMu.Lock()
	if d.navDone != nil {
		d.navMu.Unlock()
		return errors.New("webview2: navigation already in progress")
	}
	d.navDone = wait
	d.navMu.Unlock()
	if err := d.dispatch(ctx, func() error { d.chromium.NavigateToString(html); return nil }); err != nil {
		d.navMu.Lock()
		d.navDone = nil
		d.navMu.Unlock()
		return err
	}
	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.done:
		return errors.New("webview2: native host stopped during navigation")
	}
}

func (d *windowsDriver) evaluate(ctx context.Context, script string) (json.RawMessage, error) {
	id := d.nextID.Add(1)
	result := make(chan evaluation, 1)
	d.pendingMu.Lock()
	d.pending[id] = result
	d.pendingMu.Unlock()
	quoted := strconv.Quote(script)
	wrapper := fmt.Sprintf(`(()=>{const id=%d;const send=(ok,value,error)=>{try{window.chrome.webview.postMessage(JSON.stringify({__rush:"evaluate",id,ok,value:value===undefined?null:value,error}))}catch(serializationError){window.chrome.webview.postMessage(JSON.stringify({__rush:"evaluate",id,ok:false,error:String(serializationError&&serializationError.stack||serializationError)}))}};try{Promise.resolve((0,eval)(%s)).then(value=>send(true,value),error=>send(false,null,String(error&&error.stack||error)))}catch(error){send(false,null,String(error&&error.stack||error))}})()`, id, quoted)
	if err := d.dispatch(ctx, func() error { d.chromium.Eval(wrapper); return nil }); err != nil {
		d.pendingMu.Lock()
		delete(d.pending, id)
		d.pendingMu.Unlock()
		return nil, err
	}
	select {
	case response := <-result:
		return response.value, response.err
	case <-ctx.Done():
		d.pendingMu.Lock()
		delete(d.pending, id)
		d.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-d.done:
		return nil, errors.New("webview2: native host stopped during evaluation")
	}
}

func (d *windowsDriver) message(message string) {
	raw := json.RawMessage(message)
	var envelope internalEnvelope
	if json.Unmarshal(raw, &envelope) == nil && envelope.Kind == "evaluate" {
		d.pendingMu.Lock()
		wait := d.pending[envelope.ID]
		delete(d.pending, envelope.ID)
		d.pendingMu.Unlock()
		if wait != nil {
			if envelope.OK {
				wait <- evaluation{value: envelope.Value}
			} else {
				wait <- evaluation{err: errors.New(envelope.Error)}
			}
		}
		return
	}
	if !json.Valid(raw) {
		encoded, _ := json.Marshal(message)
		raw = encoded
	}
	if d.onMessage != nil {
		d.onMessage(raw)
	}
}

func (d *windowsDriver) capturePNG(ctx context.Context) ([]byte, error) {
	restoreHidden := d.config.Mode == ModeHidden
	if restoreHidden {
		if err := d.dispatch(ctx, func() error {
			procShowWindow.Call(d.hwnd, swShow)
			if err := d.chromium.Show(); err != nil {
				return err
			}
			procUpdateWindow.Call(d.hwnd)
			return nil
		}); err != nil {
			return nil, err
		}
		defer func() {
			_ = d.dispatch(context.Background(), func() error {
				_ = d.chromium.Hide()
				procShowWindow.Call(d.hwnd, swHide)
				return nil
			})
		}()
	}
	var stream *rawStream
	if hr, _, _ := procCreateStreamGlobal.Call(0, 1, uintptr(unsafe.Pointer(&stream))); int32(hr) < 0 {
		return nil, fmt.Errorf("webview2: CreateStreamOnHGlobal failed: %w", syscall.Errno(hr))
	}
	defer stream.release()
	completed := make(chan error, 1)
	handler := newCaptureHandler(completed)
	err := d.dispatch(ctx, func() error {
		view := (*chromiumLayout)(unsafe.Pointer(d.chromium)).webview
		if view == nil {
			return errors.New("webview2: native view is unavailable")
		}
		raw := (*rawCoreWebView)(unsafe.Pointer(view))
		hr, _, _ := syscall.SyscallN(raw.vtbl.CapturePreview, uintptr(unsafe.Pointer(raw)), 0, uintptr(unsafe.Pointer(stream)), uintptr(unsafe.Pointer(handler)))
		if int32(hr) < 0 {
			return syscall.Errno(hr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case err := <-completed:
		if err != nil {
			return nil, err
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.done:
		return nil, errors.New("webview2: native host stopped during screenshot")
	}
	runtime.KeepAlive(handler)
	return bytesFromStream(stream)
}

type captureHandler struct {
	vtbl      *captureHandlerVtbl
	refs      atomic.Uint32
	completed chan error
}

type captureHandlerVtbl struct{ QueryInterface, AddRef, Release, Invoke uintptr }

var captureCallbacks = captureHandlerVtbl{
	QueryInterface: windows.NewCallback(captureQueryInterface),
	AddRef:         windows.NewCallback(captureAddRef),
	Release:        windows.NewCallback(captureRelease),
	Invoke:         windows.NewCallback(captureInvoke),
}

func newCaptureHandler(completed chan error) *captureHandler {
	h := &captureHandler{vtbl: &captureCallbacks, completed: completed}
	h.refs.Store(1)
	return h
}

func captureQueryInterface(this, _, object uintptr) uintptr {
	if object == 0 {
		return uintptr(windows.E_POINTER)
	}
	*(*uintptr)(unsafe.Pointer(object)) = this
	captureAddRef(this)
	return uintptr(windows.S_OK)
}
func captureAddRef(this uintptr) uintptr {
	return uintptr((*captureHandler)(unsafe.Pointer(this)).refs.Add(1))
}
func captureRelease(this uintptr) uintptr {
	return uintptr((*captureHandler)(unsafe.Pointer(this)).refs.Add(^uint32(0)))
}
func captureInvoke(this, errorCode uintptr) uintptr {
	h := (*captureHandler)(unsafe.Pointer(this))
	if int32(errorCode) < 0 {
		h.completed <- syscall.Errno(errorCode)
	} else {
		h.completed <- nil
	}
	return 0
}

func bytesFromStream(stream *rawStream) ([]byte, error) {
	var handle uintptr
	if hr, _, _ := procGetHGlobalStream.Call(uintptr(unsafe.Pointer(stream)), uintptr(unsafe.Pointer(&handle))); int32(hr) < 0 {
		return nil, syscall.Errno(hr)
	}
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, errors.New("webview2: screenshot stream was empty")
	}
	pointer, _, err := procGlobalLock.Call(handle)
	if pointer == 0 {
		return nil, fmt.Errorf("webview2: GlobalLock failed: %w", err)
	}
	defer procGlobalUnlock.Call(handle)
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(pointer)), int(size))...), nil
}

func (d *windowsDriver) trustedMouse(ctx context.Context, action MouseAction) error {
	if action.X < 0 || action.Y < 0 || action.X >= d.config.Width || action.Y >= d.config.Height {
		return errors.New("webview2: trusted mouse coordinates are outside the viewport")
	}
	if action.Button < MouseButtonLeft || action.Button > MouseButtonMiddle {
		return errors.New("webview2: trusted mouse button is required")
	}
	clicks := action.Clicks
	if clicks == 0 {
		clicks = 1
	}
	if clicks < 1 || clicks > 3 {
		return errors.New("webview2: trusted mouse clicks must be between 1 and 3")
	}
	return d.withNativeFocus(ctx, func() error {
		point := winPoint{X: int32(action.X), Y: int32(action.Y)}
		if ok, _, err := procClientToScreen.Call(d.hwnd, uintptr(unsafe.Pointer(&point))); ok == 0 {
			return fmt.Errorf("webview2: ClientToScreen failed: %w", err)
		}
		x0 := int32(metric(smXVirtualScreen))
		y0 := int32(metric(smYVirtualScreen))
		width := int32(metric(smCXVirtualScreen))
		height := int32(metric(smCYVirtualScreen))
		move := mouseInput{Type: inputMouse, X: int32((int64(point.X-x0) * 65535) / int64(width-1)), Y: int32((int64(point.Y-y0) * 65535) / int64(height-1)), Flags: mouseeventfMove | mouseeventfAbsolute | mouseeventfVirtualDesk}
		if err := sendMouse(move); err != nil {
			return err
		}
		down, up := uint32(mouseeventfLeftDown), uint32(mouseeventfLeftUp)
		if action.Button == MouseButtonRight {
			down, up = mouseeventfRightDown, mouseeventfRightUp
		}
		if action.Button == MouseButtonMiddle {
			down, up = mouseeventfMiddleDown, mouseeventfMiddleUp
		}
		for range clicks {
			if err := sendMouse(mouseInput{Type: inputMouse, Flags: down}); err != nil {
				return err
			}
			if err := sendMouse(mouseInput{Type: inputMouse, Flags: up}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *windowsDriver) trustedKey(ctx context.Context, action KeyAction) error {
	if action.Text == "" && action.Key == 0 {
		return errors.New("webview2: trusted key requires text or a virtual-key code")
	}
	return d.withNativeFocus(ctx, func() error {
		modifiers := []struct {
			mask KeyModifiers
			key  uint16
		}{{KeyModifierShift, 0x10}, {KeyModifierControl, 0x11}, {KeyModifierAlt, 0x12}, {KeyModifierMeta, 0x5B}}
		for _, modifier := range modifiers {
			if action.Modifiers&modifier.mask != 0 {
				if err := sendKey(keyboardInput{Type: inputKeyboard, VirtualKey: modifier.key}); err != nil {
					return err
				}
			}
		}
		defer func() {
			for index := len(modifiers) - 1; index >= 0; index-- {
				modifier := modifiers[index]
				if action.Modifiers&modifier.mask != 0 {
					_ = sendKey(keyboardInput{Type: inputKeyboard, VirtualKey: modifier.key, Flags: keyeventfKeyUp})
				}
			}
		}()
		if action.Text != "" {
			for _, code := range utf16.Encode([]rune(action.Text)) {
				if err := sendKey(keyboardInput{Type: inputKeyboard, ScanCode: code, Flags: keyeventfUnicode}); err != nil {
					return err
				}
				if err := sendKey(keyboardInput{Type: inputKeyboard, ScanCode: code, Flags: keyeventfUnicode | keyeventfKeyUp}); err != nil {
					return err
				}
			}
			return nil
		}
		if err := sendKey(keyboardInput{Type: inputKeyboard, VirtualKey: action.Key}); err != nil {
			return err
		}
		return sendKey(keyboardInput{Type: inputKeyboard, VirtualKey: action.Key, Flags: keyeventfKeyUp})
	})
}

func (d *windowsDriver) withNativeFocus(ctx context.Context, operation func() error) error {
	restoreHidden := d.config.Mode == ModeHidden
	err := d.dispatch(ctx, func() error {
		if restoreHidden {
			procSetWindowPos.Call(d.hwnd, 0, 0, 0, 0, 0, swpNoSize|swpNoZOrder)
			procShowWindow.Call(d.hwnd, swShow)
			_ = d.chromium.Show()
			procUpdateWindow.Call(d.hwnd)
		}
		foreground, _, _ := procGetForegroundWindow.Call()
		foregroundThread, _, _ := procGetWindowThreadID.Call(foreground, 0)
		currentThread, _, _ := procGetCurrentThreadID.Call()
		if foregroundThread != 0 && foregroundThread != currentThread {
			if attached, _, err := procAttachThreadInput.Call(currentThread, foregroundThread, 1); attached == 0 {
				return fmt.Errorf("webview2: AttachThreadInput failed: %w", err)
			}
			defer procAttachThreadInput.Call(currentThread, foregroundThread, 0)
		}
		procBringWindowToTop.Call(d.hwnd)
		procSetActiveWindow.Call(d.hwnd)
		if ok, _, _ := procSetForegroundWindow.Call(d.hwnd); ok == 0 {
			return errors.New("webview2: Windows denied foreground focus for trusted input")
		}
		procSetFocus.Call(d.hwnd)
		d.chromium.Focus()
		return operation()
	})
	if err != nil {
		return err
	}
	// SendInput queues work to Chromium's renderer process. Keep the controller
	// rendered until that queue has observed the native input before restoring a
	// normal hidden run; otherwise Windows can discard the pending event.
	timer := time.NewTimer(50 * time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}
	if restoreHidden {
		return d.dispatch(ctx, func() error {
			_ = d.chromium.Hide()
			procShowWindow.Call(d.hwnd, swHide)
			procSetWindowPos.Call(d.hwnd, 0, winCoord(-32000), winCoord(-32000), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
			return nil
		})
	}
	return nil
}

func sendMouse(input mouseInput) error {
	if sent, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input)); sent != 1 {
		return fmt.Errorf("webview2: SendInput(mouse) failed: %w", err)
	}
	return nil
}

func sendKey(input keyboardInput) error {
	if sent, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&input)), unsafe.Sizeof(input)); sent != 1 {
		return fmt.Errorf("webview2: SendInput(keyboard) failed: %w", err)
	}
	return nil
}

func metric(index int32) int {
	value, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(value)
}

func winCoord(value int32) uintptr { return uintptr(uint32(value)) }

func (d *windowsDriver) close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return nil
	}
	if d.hwnd == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	call := uiCall{fn: func() error { d.chromium.ShuttingDown(); procDestroyWindow.Call(d.hwnd); return nil }, done: make(chan error, 1)}
	select {
	case d.calls <- call:
	default:
		return errors.New("webview2: native close queue is full")
	}
	procPostMessageW.Call(d.hwnd, wmRushDispatch, 0, 0)
	select {
	case err := <-call.done:
		<-d.done
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
