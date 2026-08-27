//go:build rush_lightpanda && linux

package lightpanda

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	webview "github.com/moxcomic/go-webview"
)

const (
	bindingTransport   = "__rushLightpandaCall"
	processOutputLimit = 64 << 10
)

const bindingBootstrap = `(() => {
  globalThis.__rushLightpanda = true;
  const pending = new Map();
  let next = 0;
  globalThis.__rushLightpandaInvoke = (name, args) => new Promise((resolve, reject) => {
    const id = String(++next);
    pending.set(id, {resolve, reject});
    globalThis.__rushLightpandaCall(JSON.stringify({id, name, args}));
  });
  globalThis.__rushLightpandaResolve = (id, ok, value) => {
    const entry = pending.get(id);
    if (!entry) return;
    pending.delete(id);
    if (ok) entry.resolve(value);
    else entry.reject(new Error(String(value)));
  };
})();`

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpMessage struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *cdpError       `json:"error,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
}

type bindingRequest struct {
	ID   string            `json:"id"`
	Name string            `json:"name"`
	Args []json.RawMessage `json:"args"`
}

type bindingEvent struct {
	Name               string `json:"name"`
	Payload            string `json:"payload"`
	ExecutionContextID int64  `json:"executionContextId"`
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(value)
	if len(value) >= processOutputLimit {
		b.buffer.Reset()
		_, _ = b.buffer.Write(value[len(value)-processOutputLimit:])
		return written, nil
	}
	if overflow := b.buffer.Len() + len(value) - processOutputLimit; overflow > 0 {
		b.buffer.Next(overflow)
	}
	_, _ = b.buffer.Write(value)
	return written, nil
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

type WebView struct {
	conn        *websocket.Conn
	process     *exec.Cmd
	processIO   synchronizedBuffer
	processDone chan error
	sessionID   string

	nextID   atomic.Int64
	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[int64]chan cdpMessage
	bindings map[string]any
	done     chan struct{}
	close    sync.Once
	destroy  sync.Once
}

func New(debug bool) (webview.WebView, error) {
	if debug {
		return nil, errors.New("Lightpanda does not support headed debug mode")
	}
	executable := os.Getenv("RUSH_LIGHTPANDA_PATH")
	if executable == "" {
		executable = "lightpanda"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return nil, errors.New("Lightpanda was not found; set RUSH_LIGHTPANDA_PATH to the nightly executable")
	}
	port, err := availablePort()
	if err != nil {
		return nil, err
	}
	process := exec.Command(path, "serve", "--log-level", "warn", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	process.Env = append(os.Environ(), "LIGHTPANDA_DISABLE_TELEMETRY=true", "LIGHTPANDA_DISABLE_CORE_DUMP=1")
	view := &WebView{
		process:     process,
		pending:     make(map[int64]chan cdpMessage),
		bindings:    make(map[string]any),
		done:        make(chan struct{}),
		processDone: make(chan error, 1),
	}
	process.Stdout = &view.processIO
	process.Stderr = &view.processIO
	if err := process.Start(); err != nil {
		return nil, fmt.Errorf("start Lightpanda: %w", err)
	}
	go func() { view.processDone <- process.Wait() }()
	endpoint, err := waitForEndpoint(port, view.processDone, &view.processIO)
	if err != nil {
		view.stopProcess()
		return nil, err
	}
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		view.stopProcess()
		return nil, fmt.Errorf("connect to Lightpanda CDP: %w", err)
	}
	view.conn = conn
	go view.readLoop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := view.call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"}, "", &target); err != nil {
		view.Destroy()
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := view.call(ctx, "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, "", &attached); err != nil {
		view.Destroy()
		return nil, err
	}
	view.sessionID = attached.SessionID
	for _, method := range []string{"Runtime.enable", "Page.enable"} {
		if err := view.call(ctx, method, nil, view.sessionID, nil); err != nil {
			view.Destroy()
			return nil, err
		}
	}
	if err := view.call(ctx, "Runtime.addBinding", map[string]any{"name": bindingTransport}, view.sessionID, nil); err != nil {
		view.Destroy()
		return nil, err
	}
	if err := view.addInitScript(ctx, bindingBootstrap); err != nil {
		view.Destroy()
		return nil, err
	}
	return view, nil
}

func availablePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve Lightpanda port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForEndpoint(port int, processDone <-chan error, output fmt.Stringer) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(url)
		if err == nil {
			var version struct {
				Endpoint string `json:"webSocketDebuggerUrl"`
			}
			decodeErr := json.NewDecoder(response.Body).Decode(&version)
			_ = response.Body.Close()
			if decodeErr == nil && version.Endpoint != "" {
				return version.Endpoint, nil
			}
		}
		select {
		case processErr := <-processDone:
			return "", fmt.Errorf("Lightpanda exited before CDP became ready (%v): %s", processErr, output.String())
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	return "", fmt.Errorf("Lightpanda CDP did not become ready: %s", output.String())
}

func (w *WebView) call(ctx context.Context, method string, params any, sessionID string, result any) error {
	id := w.nextID.Add(1)
	reply := make(chan cdpMessage, 1)
	w.mu.Lock()
	w.pending[id] = reply
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		delete(w.pending, id)
		w.mu.Unlock()
	}()
	request := struct {
		ID        int    `json:"id"`
		Method    string `json:"method"`
		Params    any    `json:"params,omitempty"`
		SessionID string `json:"sessionId,omitempty"`
	}{ID: int(id), Method: method, Params: params, SessionID: sessionID}
	w.writeMu.Lock()
	err := w.conn.WriteJSON(request)
	w.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("send Lightpanda %s: %w", method, err)
	}
	select {
	case response := <-reply:
		if response.Error != nil {
			return fmt.Errorf("Lightpanda %s failed (%d): %s", method, response.Error.Code, response.Error.Message)
		}
		if result != nil && len(response.Result) > 0 {
			if err := json.Unmarshal(response.Result, result); err != nil {
				return fmt.Errorf("decode Lightpanda %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for Lightpanda %s: %w", method, ctx.Err())
	case <-w.done:
		return fmt.Errorf("Lightpanda closed during %s: %s", method, w.processIO.String())
	}
}

func (w *WebView) readLoop() {
	defer w.Terminate()
	for {
		var message cdpMessage
		if err := w.conn.ReadJSON(&message); err != nil {
			return
		}
		if message.ID != 0 {
			w.mu.Lock()
			pending := w.pending[message.ID]
			w.mu.Unlock()
			if pending != nil {
				pending <- message
			}
			continue
		}
		if message.Method == "Runtime.bindingCalled" {
			var event bindingEvent
			if json.Unmarshal(message.Params, &event) == nil && event.Name == bindingTransport {
				go w.handleBinding(message.SessionID, event)
			}
			continue
		}
		if message.Method == "Runtime.exceptionThrown" {
			fmt.Fprintf(os.Stderr, "Lightpanda page exception: %s\n", message.Params)
		}
	}
}

func (w *WebView) handleBinding(sessionID string, event bindingEvent) {
	var request bindingRequest
	if err := json.Unmarshal([]byte(event.Payload), &request); err != nil {
		return
	}
	w.mu.Lock()
	callback := w.bindings[request.Name]
	w.mu.Unlock()
	var value any
	var callErr error
	if callback == nil {
		callErr = fmt.Errorf("unknown binding %q", request.Name)
	} else {
		value, callErr = invoke(callback, request.Args)
	}
	if callErr != nil {
		value = callErr.Error()
	}
	idJSON, _ := json.Marshal(request.ID)
	valueJSON, marshalErr := json.Marshal(value)
	if marshalErr != nil {
		callErr = fmt.Errorf("encode binding result: %w", marshalErr)
		valueJSON, _ = json.Marshal(callErr.Error())
	}
	script := fmt.Sprintf("globalThis.__rushLightpandaResolve(%s,%t,%s)", idJSON, callErr == nil, valueJSON)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	params := map[string]any{"expression": script, "awaitPromise": false, "returnByValue": true}
	if event.ExecutionContextID != 0 {
		params["contextId"] = event.ExecutionContextID
	}
	_ = w.call(ctx, "Runtime.evaluate", params, sessionID, nil)
}

func invoke(callback any, args []json.RawMessage) (value any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("binding panic: %v", recovered)
		}
	}()
	function := reflect.ValueOf(callback)
	if !function.IsValid() {
		return nil, errors.New("binding callback must be a function")
	}
	typeOf := function.Type()
	if typeOf.Kind() != reflect.Func {
		return nil, errors.New("binding callback must be a function")
	}
	if typeOf.NumIn() != len(args) {
		return nil, fmt.Errorf("binding expects %d arguments, received %d", typeOf.NumIn(), len(args))
	}
	inputs := make([]reflect.Value, len(args))
	for index, raw := range args {
		input := reflect.New(typeOf.In(index))
		if err := json.Unmarshal(raw, input.Interface()); err != nil {
			return nil, fmt.Errorf("decode binding argument %d: %w", index+1, err)
		}
		inputs[index] = input.Elem()
	}
	outputs := function.Call(inputs)
	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		if typeOf.Out(0).Implements(reflect.TypeFor[error]()) {
			if outputs[0].IsNil() {
				return nil, nil
			}
			return nil, outputs[0].Interface().(error)
		}
		return outputs[0].Interface(), nil
	case 2:
		if !typeOf.Out(1).Implements(reflect.TypeFor[error]()) {
			return nil, errors.New("binding's second result must be an error")
		}
		if !outputs[1].IsNil() {
			return nil, outputs[1].Interface().(error)
		}
		return outputs[0].Interface(), nil
	default:
		return nil, errors.New("binding must return at most a value and an error")
	}
}

func (w *WebView) addInitScript(ctx context.Context, source string) error {
	return w.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": source}, w.sessionID, nil)
}

func (w *WebView) Run() { <-w.done }

func (w *WebView) Terminate() { w.close.Do(func() { close(w.done) }) }

func (w *WebView) Dispatch(function func()) { function() }

func (w *WebView) Destroy() {
	w.destroy.Do(func() {
		w.Terminate()
		if w.conn != nil {
			_ = w.conn.Close()
		}
		w.stopProcess()
	})
}

func (w *WebView) stopProcess() {
	if w.process == nil || w.process.Process == nil || w.process.ProcessState != nil {
		return
	}
	_ = w.process.Process.Signal(os.Interrupt)
	select {
	case <-w.processDone:
	case <-time.After(time.Second):
		_ = w.process.Process.Kill()
		<-w.processDone
	}
}

func (w *WebView) Window() unsafe.Pointer { return nil }

func (w *WebView) SetTitle(string) {}

func (w *WebView) SetSize(int, int, webview.Hint) {}

func (w *WebView) Navigate(url string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := w.call(ctx, "Page.navigate", map[string]any{"url": url}, w.sessionID, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
}

func (w *WebView) SetHtml(html string) {
	encoded := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html))
	w.Navigate(encoded)
}

func (w *WebView) Init(source string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.addInitScript(ctx, source); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (w *WebView) Eval(source string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		sourceJSON, _ := json.Marshal(source)
		expression := fmt.Sprintf("void (0,eval)(%s)", sourceJSON)
		var result struct {
			Exception json.RawMessage `json:"exceptionDetails"`
		}
		if err := w.call(ctx, "Runtime.evaluate", map[string]any{"expression": expression, "awaitPromise": false, "returnByValue": false}, w.sessionID, &result); err != nil {
			fmt.Fprintln(os.Stderr, err)
		} else if len(result.Exception) > 0 {
			fmt.Fprintf(os.Stderr, "Lightpanda evaluation exception: %s\n", result.Exception)
		}
	}()
}

func (w *WebView) Bind(name string, callback any) error {
	callbackType := reflect.TypeOf(callback)
	if callbackType == nil || callbackType.Kind() != reflect.Func {
		return errors.New("binding callback must be a function")
	}
	w.mu.Lock()
	if _, exists := w.bindings[name]; exists {
		w.mu.Unlock()
		return fmt.Errorf("binding %q already exists", name)
	}
	w.bindings[name] = callback
	w.mu.Unlock()
	nameJSON, _ := json.Marshal(name)
	source := fmt.Sprintf("globalThis[%s]=(...args)=>globalThis.__rushLightpandaInvoke(%s,args)", nameJSON, nameJSON)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.addInitScript(ctx, source); err != nil {
		return err
	}
	return w.call(ctx, "Runtime.evaluate", map[string]any{"expression": source}, w.sessionID, nil)
}

func (w *WebView) Unbind(name string) error {
	w.mu.Lock()
	delete(w.bindings, name)
	w.mu.Unlock()
	nameJSON, _ := json.Marshal(name)
	w.Eval(fmt.Sprintf("delete globalThis[%s]", nameJSON))
	return nil
}
