//go:build darwin && cgo

package wkwebview

/*
#cgo CFLAGS: -x objective-c -fblocks -fobjc-arc -mmacosx-version-min=13.0
#cgo LDFLAGS: -framework AppKit -framework WebKit -framework ApplicationServices -framework CoreGraphics
#include <stdlib.h>
#include "wkwebview.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"

	webview "github.com/moxcomic/go-webview"
)

var errorType = reflect.TypeOf((*error)(nil)).Elem()

type View struct {
	native   *C.rush_wk_view
	handle   cgo.Handle
	mu       sync.RWMutex
	bindings map[string]reflect.Value
	closed   bool
	wait     sync.WaitGroup
}

type evaluation struct {
	value json.RawMessage
	err   error
}

type snapshot struct {
	data []byte
	err  error
}

type FailureArtifacts struct {
	ScreenshotPath string
	DOMPath        string
}

func New(debug bool) (webview.WebView, error) {
	view := &View{bindings: make(map[string]reflect.Value)}
	view.handle = cgo.NewHandle(view)
	debugValue := C.int(0)
	if debug {
		debugValue = 1
	}
	view.native = C.rush_wk_create(debugValue, C.uintptr_t(view.handle))
	if view.native == nil {
		view.handle.Delete()
		return nil, errors.New("wkwebview: create WKWebView on the main thread")
	}
	return view, nil
}

func (v *View) Run()       { C.rush_wk_run(v.native) }
func (v *View) Terminate() { C.rush_wk_terminate(v.native) }

func (v *View) Dispatch(callback func()) {
	handle := cgo.NewHandle(callback)
	C.rush_wk_dispatch(v.native, C.uintptr_t(handle))
}

func (v *View) Destroy() {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.closed = true
	v.mu.Unlock()
	v.wait.Wait()
	C.rush_wk_destroy(v.native)
	v.handle.Delete()
}

func (v *View) Window() unsafe.Pointer { return C.rush_wk_window(v.native) }

func withCString(value string, callback func(*C.char)) {
	cString := C.CString(value)
	defer C.free(unsafe.Pointer(cString))
	callback(cString)
}

func (v *View) SetTitle(title string) {
	withCString(title, func(value *C.char) { C.rush_wk_set_title(v.native, value) })
}

func (v *View) SetSize(width, height int, hint webview.Hint) {
	C.rush_wk_set_size(v.native, C.int(width), C.int(height), C.int(hint))
}

func (v *View) Navigate(url string) {
	withCString(url, func(value *C.char) { C.rush_wk_navigate(v.native, value) })
}

func (v *View) SetHtml(html string) {
	withCString(html, func(value *C.char) { C.rush_wk_set_html(v.native, value) })
}

func (v *View) Init(javascript string) {
	withCString(javascript, func(value *C.char) { C.rush_wk_init(v.native, value) })
}

func (v *View) Eval(javascript string) {
	withCString(javascript, func(value *C.char) { C.rush_wk_eval(v.native, value) })
}

func (v *View) Bind(name string, callback any) error {
	value := reflect.ValueOf(callback)
	if value.Kind() != reflect.Func {
		return fmt.Errorf("wkwebview: binding %q must be a function", name)
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("wkwebview: binding name must not be empty")
	}
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return errors.New("wkwebview: view is closed")
	}
	if _, exists := v.bindings[name]; exists {
		v.mu.Unlock()
		return fmt.Errorf("wkwebview: binding %q already exists", name)
	}
	v.bindings[name] = value
	v.mu.Unlock()
	encoded, _ := json.Marshal(name)
	v.Init(`(() => {
  const state = globalThis.__rushWKBindings ||= {next: 0, pending: new Map()};
  globalThis.__rushWKResolve ||= ((id, ok, value) => {
    const pending = state.pending.get(id);
    if (!pending) return;
    state.pending.delete(id);
    (ok ? pending.resolve : pending.reject)(ok ? value : new Error(String(value)));
  });
  globalThis[` + string(encoded) + `] = (...args) => new Promise((resolve, reject) => {
    const id = ++state.next;
    state.pending.set(id, {resolve, reject});
    window.webkit.messageHandlers.rush.postMessage(JSON.stringify({id, name: ` + string(encoded) + `, args}));
  });
})()`)
	return nil
}

func (v *View) Unbind(name string) error {
	v.mu.Lock()
	if _, exists := v.bindings[name]; !exists {
		v.mu.Unlock()
		return fmt.Errorf("wkwebview: binding %q does not exist", name)
	}
	delete(v.bindings, name)
	v.mu.Unlock()
	encoded, _ := json.Marshal(name)
	v.Eval(`delete globalThis[` + string(encoded) + `]`)
	return nil
}

func (v *View) receive(raw string) {
	var request struct {
		ID   uint64            `json:"id"`
		Name string            `json:"name"`
		Args []json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return
	}
	v.mu.RLock()
	callback := v.bindings[request.Name]
	closed := v.closed
	if closed || !callback.IsValid() {
		v.mu.RUnlock()
		v.reply(request.ID, nil, fmt.Errorf("unknown native binding %q", request.Name))
		return
	}
	v.wait.Add(1)
	v.mu.RUnlock()
	go func() {
		defer v.wait.Done()
		result, err := call(callback, request.Args)
		v.reply(request.ID, result, err)
	}()
}

func call(callback reflect.Value, arguments []json.RawMessage) (result any, resultErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = fmt.Errorf("native binding panicked: %v", recovered)
		}
	}()
	typeOfCallback := callback.Type()
	if typeOfCallback.IsVariadic() || len(arguments) != typeOfCallback.NumIn() {
		return nil, fmt.Errorf("native binding expects %d arguments, received %d", typeOfCallback.NumIn(), len(arguments))
	}
	values := make([]reflect.Value, len(arguments))
	for index, raw := range arguments {
		value := reflect.New(typeOfCallback.In(index))
		if err := json.Unmarshal(raw, value.Interface()); err != nil {
			return nil, fmt.Errorf("decode argument %d: %w", index+1, err)
		}
		values[index] = value.Elem()
	}
	outputs := callback.Call(values)
	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		if typeOfCallback.Out(0).Implements(errorType) {
			if !outputs[0].IsNil() {
				return nil, outputs[0].Interface().(error)
			}
			return nil, nil
		}
		return outputs[0].Interface(), nil
	case 2:
		if !typeOfCallback.Out(1).Implements(errorType) {
			return nil, errors.New("native binding second result must be an error")
		}
		if !outputs[1].IsNil() {
			return nil, outputs[1].Interface().(error)
		}
		return outputs[0].Interface(), nil
	default:
		return nil, errors.New("native binding must return at most a value and an error")
	}
}

func (v *View) reply(id uint64, result any, err error) {
	idJSON, _ := json.Marshal(id)
	if err != nil {
		errorJSON, _ := json.Marshal(err.Error())
		v.Eval(`globalThis.__rushWKResolve?.(` + string(idJSON) + `,false,` + string(errorJSON) + `)`)
		return
	}
	resultJSON, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		v.reply(id, nil, marshalErr)
		return
	}
	v.Eval(`globalThis.__rushWKResolve?.(` + string(idJSON) + `,true,` + string(resultJSON) + `)`)
}

func (v *View) Evaluate(ctx context.Context, javascript string) (json.RawMessage, error) {
	result := make(chan evaluation, 1)
	handle := cgo.NewHandle(result)
	withCString(javascript, func(value *C.char) { C.rush_wk_evaluate(v.native, value, C.uintptr_t(handle)) })
	select {
	case evaluated := <-result:
		return evaluated.value, evaluated.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (v *View) CapturePNG(ctx context.Context) ([]byte, error) {
	result := make(chan snapshot, 1)
	handle := cgo.NewHandle(result)
	C.rush_wk_snapshot(v.native, C.uintptr_t(handle))
	select {
	case captured := <-result:
		return captured.data, captured.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (v *View) CaptureFailure(ctx context.Context, directory, name string) (FailureArtifacts, error) {
	name = sanitize(name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return FailureArtifacts{}, err
	}
	png, pngErr := v.CapturePNG(ctx)
	domJSON, domErr := v.Evaluate(ctx, `document.documentElement ? document.documentElement.outerHTML : ""`)
	artifacts := FailureArtifacts{
		ScreenshotPath: filepath.Join(directory, name+".png"),
		DOMPath:        filepath.Join(directory, name+".html"),
	}
	var failures []error
	if pngErr != nil {
		failures = append(failures, pngErr)
	} else if err := os.WriteFile(artifacts.ScreenshotPath, png, 0o644); err != nil {
		failures = append(failures, err)
	}
	if domErr != nil {
		failures = append(failures, domErr)
	} else {
		var dom string
		if err := json.Unmarshal(domJSON, &dom); err != nil {
			failures = append(failures, err)
		} else if err := os.WriteFile(artifacts.DOMPath, []byte(dom), 0o644); err != nil {
			failures = append(failures, err)
		}
	}
	return artifacts, errors.Join(failures...)
}

func sanitize(value string) string {
	value = strings.Map(func(character rune) rune {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || strings.ContainsRune("._-", character) {
			return character
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "failure"
	}
	if len(value) > 120 {
		return value[:120]
	}
	return value
}

func ContentOrigin(window unsafe.Pointer) (float64, float64, error) {
	view := (*C.rush_wk_view)(window)
	var x, y C.double
	if code := C.rush_wk_content_origin(view, &x, &y); code != 0 {
		return 0, 0, errors.New("trusted native input requires a headed WKWebView window")
	}
	return float64(x), float64(y), nil
}

func TrustedClick(x, y float64) error {
	return inputError(C.rush_wk_trusted_click(C.double(x), C.double(y)))
}
func TrustedType(value string) error {
	var code C.int
	withCString(value, func(text *C.char) { code = C.rush_wk_trusted_type(text) })
	return inputError(code)
}
func TrustedPress(value string) error {
	var code C.int
	withCString(value, func(key *C.char) { code = C.rush_wk_trusted_press(key) })
	return inputError(code)
}

func inputError(code C.int) error {
	switch code {
	case 0:
		return nil
	case 2:
		return errors.New("trusted native input requires macOS Accessibility permission")
	case 3:
		return errors.New("Core Graphics could not create a native input event")
	case 4:
		return errors.New("unsupported native key")
	default:
		return errors.New("trusted native input is unavailable")
	}
}

//export goRushWKMessage
func goRushWKMessage(handle C.uintptr_t, message *C.char) {
	view := cgo.Handle(handle).Value().(*View)
	view.receive(C.GoString(message))
}

//export goRushWKDispatch
func goRushWKDispatch(token C.uintptr_t) {
	handle := cgo.Handle(token)
	callback := handle.Value().(func())
	handle.Delete()
	callback()
}

//export goRushWKEvaluation
func goRushWKEvaluation(token C.uintptr_t, value, nativeError *C.char) {
	handle := cgo.Handle(token)
	result := handle.Value().(chan evaluation)
	handle.Delete()
	if nativeError != nil {
		result <- evaluation{err: errors.New(C.GoString(nativeError))}
		return
	}
	result <- evaluation{value: json.RawMessage(C.GoString(value))}
}

//export goRushWKSnapshot
func goRushWKSnapshot(token C.uintptr_t, bytes unsafe.Pointer, length C.int, nativeError *C.char) {
	handle := cgo.Handle(token)
	result := handle.Value().(chan snapshot)
	handle.Delete()
	if nativeError != nil {
		result <- snapshot{err: errors.New(C.GoString(nativeError))}
		return
	}
	result <- snapshot{data: C.GoBytes(bytes, length)}
}

var _ webview.WebView = (*View)(nil)
