//go:build linux && rush_obscura

package rush

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
	"syscall"
	"time"
	"unsafe"

	"github.com/gorilla/websocket"
	webview "github.com/moxcomic/go-webview"
)

const obscuraCallTimeout = 60 * time.Second

type obscuraWebView struct {
	connection *websocket.Conn
	command    *exec.Cmd
	wait       chan error
	sessionID  string
	done       chan struct{}

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[int64]chan obscuraCDPMessage
	bindings map[string]obscuraBinding
	nextID   atomic.Int64
	pumping  atomic.Bool
	stopOnce sync.Once
	destroy  sync.Once
}

type obscuraCDPMessage struct {
	ID        int64            `json:"id,omitempty"`
	Method    string           `json:"method,omitempty"`
	SessionID string           `json:"sessionId,omitempty"`
	Params    json.RawMessage  `json:"params,omitempty"`
	Result    json.RawMessage  `json:"result,omitempty"`
	Error     *obscuraCDPError `json:"error,omitempty"`
}

type obscuraCDPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type obscuraBindingEvent struct {
	Name               string `json:"name"`
	Payload            string `json:"payload"`
	ExecutionContextID int64  `json:"executionContextId"`
}

type obscuraBindingRequest struct {
	ID   string          `json:"id"`
	Args json.RawMessage `json:"args"`
}

type obscuraBinding func(json.RawMessage) (any, error)

func obscuraBinary() (string, error) {
	configured := os.Getenv("OBSCURA_BIN")
	if configured == "" {
		configured = "obscura"
	}
	binary, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("Obscura binary was not found; set OBSCURA_BIN to the v0.2.1 no-render executable: %w", err)
	}
	return binary, nil
}

func newObscuraWebView() (*obscuraWebView, error) {
	binary, err := obscuraBinary()
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("reserve Obscura CDP port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return nil, fmt.Errorf("release Obscura CDP port: %w", err)
	}

	var processLog bytes.Buffer
	command := exec.Command(binary,
		"serve",
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--max-connections", "1",
		"--allow-private-network",
		"--quiet",
	)
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	command.Stdout = &processLog
	command.Stderr = &processLog
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start Obscura: %w", err)
	}
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, requestErr := client.Get(endpoint)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case processErr := <-wait:
			return nil, fmt.Errorf("Obscura exited before CDP was ready: %v: %s", processErr, processLog.String())
		default:
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			<-wait
			return nil, fmt.Errorf("Obscura CDP did not become ready: %s", processLog.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	connection, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser", port), nil,
	)
	if err != nil {
		_ = command.Process.Kill()
		<-wait
		return nil, fmt.Errorf("connect to Obscura CDP: %w", err)
	}
	view := &obscuraWebView{
		connection: connection,
		command:    command,
		wait:       wait,
		done:       make(chan struct{}),
		pending:    make(map[int64]chan obscuraCDPMessage),
		bindings:   make(map[string]obscuraBinding),
	}
	go view.readLoop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := view.call(ctx, "Target.createTarget", map[string]any{"url": "about:blank"}, false, &target); err != nil {
		view.Destroy()
		return nil, err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := view.call(ctx, "Target.attachToTarget", map[string]any{
		"targetId": target.TargetID,
		"flatten":  true,
	}, false, &attached); err != nil {
		view.Destroy()
		return nil, err
	}
	view.sessionID = attached.SessionID
	for _, method := range []string{"Page.enable", "Runtime.enable"} {
		if err := view.call(ctx, method, nil, true, nil); err != nil {
			view.Destroy()
			return nil, err
		}
	}
	if err := view.addInit(obscuraBindingBootstrap); err != nil {
		view.Destroy()
		return nil, err
	}
	go view.pumpPageTasks()
	return view, nil
}

func (v *obscuraWebView) pumpPageTasks() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !v.pumping.Load() {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = v.call(ctx, "Runtime.evaluate", map[string]any{"expression": "void 0"}, true, nil)
			cancel()
		case <-v.done:
			return
		}
	}
}

func (v *obscuraWebView) readLoop() {
	for {
		var message obscuraCDPMessage
		if err := v.connection.ReadJSON(&message); err != nil {
			v.failPending(err)
			v.Terminate()
			return
		}
		if message.ID != 0 {
			v.mu.Lock()
			response := v.pending[message.ID]
			delete(v.pending, message.ID)
			v.mu.Unlock()
			if response != nil {
				response <- message
			}
			continue
		}
		if message.Method == "Runtime.bindingCalled" {
			var event obscuraBindingEvent
			if json.Unmarshal(message.Params, &event) == nil {
				go v.handleBinding(event)
			}
		}
	}
}

func (v *obscuraWebView) failPending(err error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, response := range v.pending {
		response <- obscuraCDPMessage{Error: &obscuraCDPError{Code: -1, Message: err.Error()}}
		delete(v.pending, id)
	}
}

func (v *obscuraWebView) call(ctx context.Context, method string, params any, page bool, target any) error {
	id := v.nextID.Add(1)
	response := make(chan obscuraCDPMessage, 1)
	v.mu.Lock()
	v.pending[id] = response
	v.mu.Unlock()
	message := map[string]any{"id": id, "method": method}
	if params != nil {
		message["params"] = params
	}
	if page {
		message["sessionId"] = v.sessionID
	}
	v.writeMu.Lock()
	err := v.connection.WriteJSON(message)
	v.writeMu.Unlock()
	if err != nil {
		v.mu.Lock()
		delete(v.pending, id)
		v.mu.Unlock()
		return err
	}
	select {
	case reply := <-response:
		if reply.Error != nil {
			return fmt.Errorf("Obscura CDP %s failed (%d): %s", method, reply.Error.Code, reply.Error.Message)
		}
		if target != nil && len(reply.Result) > 0 {
			if err := json.Unmarshal(reply.Result, target); err != nil {
				return fmt.Errorf("decode Obscura CDP %s response: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		v.mu.Lock()
		delete(v.pending, id)
		v.mu.Unlock()
		return fmt.Errorf("Obscura CDP %s: %w", method, ctx.Err())
	case <-v.done:
		return errors.New("Obscura CDP stopped")
	}
}

func (v *obscuraWebView) handleBinding(event obscuraBindingEvent) {
	if event.Name == "__rushReport" {
		v.pumping.Store(false)
	}
	v.mu.Lock()
	binding := v.bindings[event.Name]
	v.mu.Unlock()
	if binding == nil {
		return
	}
	var request obscuraBindingRequest
	if err := json.Unmarshal([]byte(event.Payload), &request); err != nil {
		return
	}
	var value any
	var callErr error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				callErr = fmt.Errorf("bound function panicked: %v", recovered)
			}
		}()
		value, callErr = binding(request.Args)
	}()
	ok := callErr == nil
	if callErr != nil {
		value = callErr.Error()
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		ok = false
		valueJSON, _ = json.Marshal(err.Error())
	}
	nameJSON, _ := json.Marshal(event.Name)
	idJSON, _ := json.Marshal(request.ID)
	expression := fmt.Sprintf("globalThis.__rushObscuraReply(%s,%s,%t,%s)", nameJSON, idJSON, ok, valueJSON)
	ctx, cancel := context.WithTimeout(context.Background(), obscuraCallTimeout)
	defer cancel()
	_ = v.call(ctx, "Runtime.evaluate", map[string]any{
		"expression": expression,
		"contextId":  event.ExecutionContextID,
	}, true, nil)
}

func makeObscuraBinding(function any) (obscuraBinding, error) {
	value := reflect.ValueOf(function)
	if !value.IsValid() || value.Kind() != reflect.Func {
		return nil, errors.New("only functions can be bound")
	}
	typeOf := value.Type()
	if typeOf.NumOut() > 2 {
		return nil, errors.New("function may only return a value or value+error")
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	returnsError := typeOf.NumOut() == 1 && typeOf.Out(0).Implements(errorType)
	if typeOf.NumOut() == 2 && !typeOf.Out(1).Implements(errorType) {
		return nil, errors.New("second return value must implement error")
	}
	return func(raw json.RawMessage) (any, error) {
		var arguments []json.RawMessage
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, err
		}
		minimum := typeOf.NumIn()
		if typeOf.IsVariadic() {
			minimum--
		}
		if len(arguments) < minimum || (!typeOf.IsVariadic() && len(arguments) != typeOf.NumIn()) {
			return nil, errors.New("function arguments mismatch")
		}
		inputs := make([]reflect.Value, len(arguments))
		for index, argument := range arguments {
			inputIndex := index
			if typeOf.IsVariadic() && index >= typeOf.NumIn()-1 {
				inputIndex = typeOf.NumIn() - 1
			}
			inputType := typeOf.In(inputIndex)
			if typeOf.IsVariadic() && index >= typeOf.NumIn()-1 {
				inputType = inputType.Elem()
			}
			decoded := reflect.New(inputType)
			if err := json.Unmarshal(argument, decoded.Interface()); err != nil {
				return nil, err
			}
			inputs[index] = decoded.Elem()
		}
		outputs := value.Call(inputs)
		switch len(outputs) {
		case 0:
			return nil, nil
		case 1:
			if returnsError {
				returned, _ := outputs[0].Interface().(error)
				return nil, returned
			}
			return outputs[0].Interface(), nil
		case 2:
			returned, _ := outputs[1].Interface().(error)
			return outputs[0].Interface(), returned
		default:
			panic("unreachable")
		}
	}, nil
}

const obscuraBindingBootstrap = `(() => {
  if (globalThis.__rushObscuraInstallBinding) return;
  const replies = new Map();
  globalThis.__rushObscuraInstallBinding = (name, raw) => {
    let next = 0;
    const pending = new Map();
    replies.set(name, pending);
    globalThis[name] = (...args) => new Promise((resolve, reject) => {
      const id = String(++next);
      pending.set(id, {resolve, reject});
      raw(JSON.stringify({id, args}));
    });
  };
  globalThis.__rushObscuraReply = (name, id, ok, value) => {
    const pending = replies.get(name);
    const callback = pending && pending.get(id);
    if (!callback) return;
    pending.delete(id);
    if (ok) callback.resolve(value); else callback.reject(new Error(String(value)));
  };
})();`

func (v *obscuraWebView) Run() { <-v.done }

func (v *obscuraWebView) Terminate() {
	v.stopOnce.Do(func() { close(v.done) })
}

func (v *obscuraWebView) Dispatch(function func()) { function() }

func (v *obscuraWebView) Destroy() {
	v.destroy.Do(func() {
		v.Terminate()
		v.writeMu.Lock()
		_ = v.connection.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		_ = v.connection.Close()
		v.writeMu.Unlock()
		if v.command.Process != nil {
			_ = v.command.Process.Kill()
		}
		select {
		case <-v.wait:
		case <-time.After(2 * time.Second):
		}
	})
}

func (*obscuraWebView) Window() unsafe.Pointer { return nil }

func (*obscuraWebView) SetTitle(string) {}

func (*obscuraWebView) SetSize(int, int, webview.Hint) {}

func (v *obscuraWebView) Navigate(target string) {
	ctx, cancel := context.WithTimeout(context.Background(), obscuraCallTimeout)
	defer cancel()
	_ = v.call(ctx, "Page.navigate", map[string]any{"url": target}, true, nil)
}

func (v *obscuraWebView) SetHtml(html string) {
	v.Navigate("data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(html)))
}

func (v *obscuraWebView) addInit(source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return v.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": source}, true, nil)
}

func (v *obscuraWebView) Init(source string) { _ = v.addInit(source) }

func (v *obscuraWebView) Eval(source string) {
	v.pumping.Store(true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), obscuraCallTimeout)
		defer cancel()
		_ = v.call(ctx, "Runtime.evaluate", map[string]any{"expression": source}, true, nil)
	}()
}

func (v *obscuraWebView) Bind(name string, function any) error {
	binding, err := makeObscuraBinding(function)
	if err != nil {
		return err
	}
	v.mu.Lock()
	if v.bindings[name] != nil {
		v.mu.Unlock()
		return errors.New("function name already bound")
	}
	v.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := v.call(ctx, "Runtime.addBinding", map[string]any{"name": name}, true, nil); err != nil {
		return err
	}
	nameJSON, _ := json.Marshal(name)
	source := fmt.Sprintf("globalThis.__rushObscuraInstallBinding(%s,globalThis[%s])", nameJSON, nameJSON)
	if err := v.call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": source}, true, nil); err != nil {
		return err
	}
	v.mu.Lock()
	v.bindings[name] = binding
	v.mu.Unlock()
	return nil
}

func (v *obscuraWebView) Unbind(name string) error {
	v.mu.Lock()
	if v.bindings[name] == nil {
		v.mu.Unlock()
		return errors.New("function name not bound")
	}
	delete(v.bindings, name)
	v.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return v.call(ctx, "Runtime.removeBinding", map[string]any{"name": name}, true, nil)
}

var _ webview.WebView = (*obscuraWebView)(nil)
