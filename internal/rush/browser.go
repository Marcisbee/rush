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
	view     webview.WebView
	server   *http.Server
	sessions *SessionPool
	ready    chan struct{}
	once     sync.Once
	mu       sync.Mutex
	pending  map[string]chan SuiteResult
}

func NewBrowser(headed bool) (*Browser, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start browser harness server: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/" {
			http.NotFound(response, request)
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
	mux.HandleFunc("/__rush/session", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(`<!doctype html><html><body><main data-testid="client">session client</main></body></html>`))
	})
	harnessServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = harnessServer.Serve(listener) }()

	view, err := webview.New(headed)
	if err != nil {
		_ = harnessServer.Close()
		return nil, fmt.Errorf("WebKitGTK is unavailable: %w", err)
	}
	browser := &Browser{
		view:     view,
		server:   harnessServer,
		sessions: NewSessionPool(headed, defaultSessionPoolSize),
		ready:    make(chan struct{}),
		pending:  make(map[string]chan SuiteResult),
	}
	if err := view.Bind("__rushReady", func() {
		browser.once.Do(func() { close(browser.ready) })
	}); err != nil {
		view.Destroy()
		return nil, fmt.Errorf("bind browser-ready bridge: %w", err)
	}
	if err := view.Bind("__rushReport", browser.receive); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind result bridge: %w", err)
	}
	if err := view.Bind("__rushCreateSession", browser.sessions.Create); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind session creation bridge: %w", err)
	}
	if err := view.Bind("__rushSessionGoto", browser.sessions.Goto); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind session navigation bridge: %w", err)
	}
	if err := view.Bind("__rushSessionEvaluate", browser.sessions.Evaluate); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind session evaluation bridge: %w", err)
	}
	if err := view.Bind("__rushDisposeSession", browser.sessions.Dispose); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind session disposal bridge: %w", err)
	}
	view.SetTitle("Rush — WebKitGTK")
	view.SetSize(1280, 800, webview.HintNone)
	view.Navigate("http://" + listener.Addr().String() + "/")
	return browser, nil
}

func (b *Browser) Ready() <-chan struct{} { return b.ready }
func (b *Browser) RunLoop()               { b.view.Run() }
func (b *Browser) Stop()                  { b.view.Terminate() }
func (b *Browser) Close() {
	b.sessions.Close()
	b.view.Destroy()
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
