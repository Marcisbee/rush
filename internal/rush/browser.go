package rush

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moxcomic/go-webview"
)

type Browser struct {
	view           webview.WebView
	server         *http.Server
	sessions       *SessionPool
	proxy          *appProxy
	nativeInput    nativeInput
	nativeInputErr error
	ready          chan struct{}
	once           sync.Once
	mu             sync.Mutex
	pending        map[string]chan browserBatchResult
	routes         map[string]chan AppHTTPResponse
	compiled       map[string]bool
	compileOrder   []string
	bundleSources  *browserBundleSourceStore
}

const browserBundleCacheLimit = 64

type browserBatchResult struct {
	Suites         []SuiteResult `json:"suites"`
	CompiledHashes []string      `json:"compiled_hashes,omitempty"`
	BrowserMS      float64       `json:"browser_ms"`
	DeliveryMS     float64       `json:"delivery_ms"`
	ReportingMS    float64       `json:"reporting_ms"`
}

type browserBundlePayload struct {
	File      string `json:"file"`
	Hash      string `json:"hash"`
	SourceURL string `json:"source_url,omitempty"`
}

type browserBundleSourceStore struct {
	mu      sync.RWMutex
	batches map[string]map[int]browserBundleSource
}

func newBrowserBundleSourceStore() *browserBundleSourceStore {
	return &browserBundleSourceStore{batches: make(map[string]map[int]browserBundleSource)}
}

type browserBundleSource struct {
	hash   string
	source string
}

func (s *browserBundleSourceStore) Put(id string, sources map[int]browserBundleSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches[id] = sources
}

func (s *browserBundleSourceStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.batches, id)
}

func (s *browserBundleSourceStore) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/__rush/bundle/"), "/")
	if len(parts) != 2 {
		http.NotFound(response, request)
		return
	}
	id, err := url.PathUnescape(parts[0])
	if err != nil {
		http.NotFound(response, request)
		return
	}
	index, err := strconv.Atoi(parts[1])
	if err != nil {
		http.NotFound(response, request)
		return
	}
	s.mu.RLock()
	bundle, ok := s.batches[id][index]
	s.mu.RUnlock()
	if !ok {
		http.NotFound(response, request)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	hashJSON, _ := json.Marshal(bundle.hash)
	_, _ = fmt.Fprintf(response, "window.__rush.installBundle(%s,function(){\n%s\n});\n", hashJSON, bundle.source)
}

type nativeInputCapability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

const browserControllerPath = "/__rush/controller"

func NewBrowser(headed bool, sessionWarmCount ...int) (*Browser, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start browser harness server: %w", err)
	}
	origin := "http://" + listener.Addr().String()
	proxy := newAppProxy(origin)
	bundleSources := newBrowserBundleSourceStore()
	mux := http.NewServeMux()
	mux.HandleFunc(browserControllerPath, func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = response.Write([]byte(browserControllerHTML))
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
	mux.Handle("/__rush/bundle/", bundleSources)
	mux.Handle("/", proxy)
	harnessServer := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = harnessServer.Serve(listener) }()

	view, err := newWebView(headed)
	if err != nil {
		_ = harnessServer.Close()
		return nil, fmt.Errorf("%s is unavailable: %w", BackendName(), err)
	}
	input, inputErr := newNativeInput()
	warmCount := 0
	if len(sessionWarmCount) > 0 {
		warmCount = sessionWarmCount[0]
	}
	browser := &Browser{
		view:           view,
		server:         harnessServer,
		sessions:       NewSessionPool(headed, defaultSessionPoolSize, warmCount),
		proxy:          proxy,
		nativeInput:    input,
		nativeInputErr: inputErr,
		ready:          make(chan struct{}),
		pending:        make(map[string]chan browserBatchResult),
		routes:         make(map[string]chan AppHTTPResponse),
		compiled:       make(map[string]bool),
		bundleSources:  bundleSources,
	}
	proxy.decide = browser.decideRequest
	proxy.complete = browser.networkComplete
	if err := view.Bind("__rushReady", func() {
		browser.once.Do(func() { close(browser.ready) })
	}); err != nil {
		browser.sessions.Close()
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
	if err := view.Bind("__rushAppNavigate", browser.navigateApp); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind application navigation bridge: %w", err)
	}
	if err := view.Bind("__rushAppReset", browser.resetApp); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind application reset bridge: %w", err)
	}
	if err := view.Bind("__rushAppRequestResult", browser.receiveRoute); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind request interception bridge: %w", err)
	}
	if err := view.Bind("__rushNativeInput", browser.sendNativeInput); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind native input bridge: %w", err)
	}
	if err := view.Bind("__rushPrepareNativeInput", func() nativeInputCapability {
		return resolveNativeInputCapability(headed, browser.nativeInputErr)
	}); err != nil {
		browser.sessions.Close()
		view.Destroy()
		return nil, fmt.Errorf("bind native input readiness bridge: %w", err)
	}
	view.SetTitle("Rush — " + BackendName())
	view.SetSize(1280, 800, webview.HintNone)
	view.Navigate(origin + browserControllerPath)
	return browser, nil
}

func resolveNativeInputCapability(headed bool, inputErr error) nativeInputCapability {
	if inputErr != nil {
		return nativeInputCapability{Reason: inputErr.Error()}
	}
	if runtime.GOOS == "darwin" && !headed {
		return nativeInputCapability{Reason: "trusted native input requires --headed on macOS"}
	}
	return nativeInputCapability{Available: true}
}

func (b *Browser) Ready() <-chan struct{} { return b.ready }
func (b *Browser) RunLoop()               { b.view.Run() }
func (b *Browser) Stop()                  { b.view.Terminate() }
func (b *Browser) Close() {
	b.sessions.Close()
	b.view.Destroy()
	if b.nativeInput != nil {
		_ = b.nativeInput.Close()
	}
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
	payload := make([]browserBundlePayload, len(bundles))
	sources := make(map[int]browserBundleSource, len(bundles))
	for index, bundle := range bundles {
		payload[index] = browserBundlePayload{File: bundle.File, Hash: bundle.Hash}
		if known[bundle.Hash] {
			continue
		}
		sources[index] = browserBundleSource{hash: bundle.Hash, source: bundle.Source}
		payload[index].SourceURL = fmt.Sprintf("/__rush/bundle/%s/%d", url.PathEscape(id), index)
		if len(order) >= browserBundleCacheLimit {
			delete(known, order[0])
			order = order[1:]
		}
		known[bundle.Hash] = true
		order = append(order, bundle.Hash)
	}
	b.mu.Unlock()
	b.bundleSources.Put(id, sources)
	defer func() {
		b.bundleSources.Delete(id)
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
		b.rememberCompiled(bundles, batch.CompiledHashes)
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
	if request.Targeted {
		type originResult struct {
			x, y float64
			err  error
		}
		origin := make(chan originResult, 1)
		b.view.Dispatch(func() {
			x, y, err := nativeContentOrigin(b.view.Window())
			origin <- originResult{x: x, y: y, err: err}
		})
		result := <-origin
		if result.err != nil {
			return result.err
		}
		// Give the window manager a moment to apply the GTK focus request before
		// XTest targets the now-active pooled realm.
		time.Sleep(10 * time.Millisecond)
		request.X += result.x
		request.Y += result.y
	}
	return b.nativeInput.Do(request)
}
