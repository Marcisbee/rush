package rush

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/moxcomic/go-webview"
	_ "github.com/moxcomic/go-webview/embedded"
)

type Browser struct {
	view           webview.WebView
	server         *http.Server
	proxy          *appProxy
	nativeInput    nativeInput
	nativeInputErr error
	ready          chan struct{}
	once           sync.Once
	mu             sync.Mutex
	pending        map[string]chan SuiteResult
	routes         map[string]chan AppHTTPResponse
}

func NewBrowser(headed bool) (*Browser, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start browser harness server: %w", err)
	}
	origin := "http://" + listener.Addr().String()
	proxy := newAppProxy(origin)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			proxy.ServeHTTP(response, request)
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(runtimeHTML))
	})
	mux.HandleFunc("/__rush/timing", func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(10 * time.Millisecond)
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte("rush"))
	})
	harnessServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = harnessServer.Serve(listener) }()

	view, err := webview.New(headed)
	if err != nil {
		_ = harnessServer.Close()
		return nil, fmt.Errorf("WebKitGTK is unavailable: %w", err)
	}
	input, inputErr := newNativeInput()
	browser := &Browser{
		view:           view,
		server:         harnessServer,
		proxy:          proxy,
		nativeInput:    input,
		nativeInputErr: inputErr,
		ready:          make(chan struct{}),
		pending:        make(map[string]chan SuiteResult),
		routes:         make(map[string]chan AppHTTPResponse),
	}
	proxy.decide = browser.decideRequest
	proxy.complete = browser.networkComplete
	if err := view.Bind("__rushReady", func() {
		browser.once.Do(func() { close(browser.ready) })
	}); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind browser-ready bridge: %w", err)
	}
	if err := view.Bind("__rushReport", browser.receive); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind result bridge: %w", err)
	}
	if err := view.Bind("__rushAppNavigate", browser.navigateApp); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind application navigation bridge: %w", err)
	}
	if err := view.Bind("__rushAppReset", browser.resetApp); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind application reset bridge: %w", err)
	}
	if err := view.Bind("__rushAppRequestResult", browser.receiveRoute); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind request interception bridge: %w", err)
	}
	if err := view.Bind("__rushNativeInput", browser.sendNativeInput); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind native input bridge: %w", err)
	}
	view.SetTitle("Rush — WebKitGTK")
	view.SetSize(1280, 800, webview.HintNone)
	view.Navigate(origin + "/")
	return browser, nil
}

func (b *Browser) Ready() <-chan struct{} { return b.ready }
func (b *Browser) RunLoop()               { b.view.Run() }
func (b *Browser) Stop()                  { b.view.Terminate() }
func (b *Browser) Close() {
	b.view.Destroy()
	if b.nativeInput != nil {
		_ = b.nativeInput.Close()
	}
	_ = b.server.Close()
}

func (b *Browser) Run(ctx context.Context, id, filename, source string) (SuiteResult, error) {
	result := make(chan SuiteResult, 1)
	b.mu.Lock()
	b.pending[id] = result
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	idJSON, _ := json.Marshal(id)
	fileJSON, _ := json.Marshal(filename)
	sourceJSON, _ := json.Marshal(source)
	script := fmt.Sprintf("window.__rush.execute(%s,%s,%s)", idJSON, fileJSON, sourceJSON)
	b.view.Dispatch(func() { b.view.Eval(script) })

	select {
	case suite := <-result:
		return suite, nil
	case <-ctx.Done():
		return SuiteResult{}, ctx.Err()
	}
}

func (b *Browser) receive(raw string) {
	var payload struct {
		ID string `json:"id"`
		SuiteResult
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return
	}
	b.mu.Lock()
	result := b.pending[payload.ID]
	b.mu.Unlock()
	if result != nil {
		result <- payload.SuiteResult
	}
}

func (b *Browser) navigateApp(realm, target string) (string, error) {
	return b.proxy.navigate(realm, target)
}

func (b *Browser) resetApp(realm string) {
	b.proxy.reset(realm)
}

func (b *Browser) decideRequest(ctx context.Context, request AppHTTPRequest) (AppHTTPResponse, error) {
	result := make(chan AppHTTPResponse, 1)
	b.mu.Lock()
	b.routes[request.ID] = result
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.routes, request.ID)
		b.mu.Unlock()
	}()
	payload := marshalAppRequest(request)
	b.view.Dispatch(func() { b.view.Eval("window.__rush.handleRequest(" + payload + ")") })
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case decision := <-result:
		return decision, nil
	case <-timeout.Done():
		return AppHTTPResponse{}, fmt.Errorf("wait for Rush request route %s: %w", request.URL, timeout.Err())
	}
}

func (b *Browser) receiveRoute(id, raw string) error {
	var decision AppHTTPResponse
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return fmt.Errorf("decode Rush route result: %w", err)
	}
	b.mu.Lock()
	result := b.routes[id]
	b.mu.Unlock()
	if result != nil {
		result <- decision
	}
	return nil
}

func (b *Browser) networkComplete(realm string, duration time.Duration) {
	realmJSON, _ := json.Marshal(realm)
	b.view.Dispatch(func() {
		b.view.Eval(fmt.Sprintf("window.__rush.networkComplete(%s,%f)", realmJSON, milliseconds(duration)))
	})
}

func (b *Browser) sendNativeInput(raw string) error {
	if b.nativeInputErr != nil {
		return b.nativeInputErr
	}
	if b.nativeInput == nil {
		return fmt.Errorf("trusted native input is unavailable")
	}
	var request NativeInputRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		return fmt.Errorf("decode native input request: %w", err)
	}
	return b.nativeInput.Do(request)
}
