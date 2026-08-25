package rush

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/moxcomic/go-webview"
)

type Browser struct {
	view         webview.WebView
	server       *http.Server
	ready        chan struct{}
	once         sync.Once
	mu           sync.Mutex
	pending      map[string]chan browserBatchResult
	compiled     map[string]bool
	compileOrder []string
}

const browserBundleCacheLimit = 64

type browserBatchResult struct {
	Suites         []SuiteResult `json:"suites"`
	CompiledHashes []string      `json:"compiled_hashes,omitempty"`
	BrowserMS      float64       `json:"browser_ms"`
	ReportingMS    float64       `json:"reporting_ms"`
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
	harnessServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = harnessServer.Serve(listener) }()

	view, err := webview.New(headed)
	if err != nil {
		_ = harnessServer.Close()
		return nil, fmt.Errorf("%s is unavailable: %w", BackendName(), err)
	}
	browser := &Browser{
		view:     view,
		server:   harnessServer,
		ready:    make(chan struct{}),
		pending:  make(map[string]chan browserBatchResult),
		compiled: make(map[string]bool),
	}
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
	view.SetTitle("Rush — " + BackendName())
	view.SetSize(1280, 800, webview.HintNone)
	view.Navigate("http://" + listener.Addr().String() + "/")
	return browser, nil
}

func (b *Browser) Ready() <-chan struct{} { return b.ready }
func (b *Browser) RunLoop()               { b.view.Run() }
func (b *Browser) Stop()                  { b.view.Terminate() }
func (b *Browser) Close() {
	b.view.Destroy()
	_ = b.server.Close()
}

func (b *Browser) Run(ctx context.Context, id, filename, source string) (SuiteResult, error) {
	digest := sha256.Sum256([]byte(source))
	batch, err := b.RunBatch(ctx, id, []BuiltSuite{{File: filename, Source: source, Hash: fmt.Sprintf("%x", digest)}})
	if err != nil {
		return SuiteResult{}, err
	}
	if len(batch.Suites) != 1 {
		return SuiteResult{}, fmt.Errorf("browser returned %d suites for one input", len(batch.Suites))
	}
	return batch.Suites[0], nil
}

func (b *Browser) RunBatch(ctx context.Context, id string, bundles []BuiltSuite) (browserBatchResult, error) {
	result := make(chan browserBatchResult, 1)
	b.mu.Lock()
	b.pending[id] = result
	known := make(map[string]bool, len(b.compiled))
	for hash := range b.compiled {
		known[hash] = true
	}
	order := append([]string(nil), b.compileOrder...)
	payload := make([]BuiltSuite, len(bundles))
	for index, bundle := range bundles {
		payload[index] = bundle
		if known[bundle.Hash] {
			payload[index].Source = ""
			continue
		}
		if len(order) >= browserBundleCacheLimit {
			delete(known, order[0])
			order = order[1:]
		}
		known[bundle.Hash] = true
		order = append(order, bundle.Hash)
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	idJSON, _ := json.Marshal(id)
	payloadJSON, _ := json.Marshal(payload)
	script := fmt.Sprintf("window.__rush.executeBatch(%s,%s)", idJSON, payloadJSON)
	b.view.Dispatch(func() { b.view.Eval(script) })

	select {
	case batch := <-result:
		b.rememberCompiled(payload, batch.CompiledHashes)
		return batch, nil
	case <-ctx.Done():
		return browserBatchResult{}, ctx.Err()
	}
}

func (b *Browser) rememberCompiled(bundles []BuiltSuite, compiledHashes []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	compiled := make(map[string]bool, len(compiledHashes))
	for _, hash := range compiledHashes {
		compiled[hash] = true
	}
	for _, bundle := range bundles {
		if bundle.Source == "" || b.compiled[bundle.Hash] || !compiled[bundle.Hash] {
			continue
		}
		if len(b.compileOrder) >= browserBundleCacheLimit {
			oldest := b.compileOrder[0]
			b.compileOrder = b.compileOrder[1:]
			delete(b.compiled, oldest)
		}
		b.compiled[bundle.Hash] = true
		b.compileOrder = append(b.compileOrder, bundle.Hash)
	}
}

func (b *Browser) receive(raw string) {
	var payload struct {
		ID string `json:"id"`
		browserBatchResult
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return
	}
	b.mu.Lock()
	result := b.pending[payload.ID]
	b.mu.Unlock()
	if result != nil {
		result <- payload.browserBatchResult
	}
}
