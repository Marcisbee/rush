package rush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"
)

type AppHTTPRequest struct {
	ID      string            `json:"id"`
	Realm   string            `json:"realm"`
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body,omitempty"`
}

type AppHTTPResponse struct {
	Action  string            `json:"action"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
}

type appProxy struct {
	origin string

	mu       sync.RWMutex
	realms   map[string]*appTarget
	active   string
	nextID   uint64
	decide   func(context.Context, AppHTTPRequest) (AppHTTPResponse, error)
	complete func(realm string, duration time.Duration)
}

type appTarget struct {
	base   *url.URL
	client *http.Client
}

func newAppProxy(origin string) *appProxy {
	return &appProxy{
		origin: origin,
		realms: make(map[string]*appTarget),
	}
}

func (proxy *appProxy) navigate(realm, rawURL string) (string, error) {
	if realm == "" || strings.ContainsAny(realm, "/?#") {
		return "", errors.New("invalid application realm")
	}
	target, err := url.Parse(rawURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return "", fmt.Errorf("application navigation requires an absolute HTTP URL: %q", rawURL)
	}
	if target.Scheme != "http" && target.Scheme != "https" {
		return "", fmt.Errorf("unsupported application URL scheme %q", target.Scheme)
	}

	proxy.mu.Lock()
	registered := proxy.realms[realm]
	if registered == nil {
		jar, _ := cookiejar.New(nil)
		registered = &appTarget{client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		}}
		proxy.realms[realm] = registered
	}
	registered.base = target
	proxy.active = realm
	proxy.mu.Unlock()
	return proxy.proxyURL(target), nil
}

func (proxy *appProxy) reset(realm string) {
	proxy.mu.Lock()
	delete(proxy.realms, realm)
	if proxy.active == realm {
		proxy.active = ""
	}
	proxy.mu.Unlock()
}

func (proxy *appProxy) proxyURL(target *url.URL) string {
	proxyPath := target.EscapedPath()
	if target.Path == "" {
		proxyPath = "/"
	}
	result := proxy.origin + proxyPath
	if target.RawQuery != "" {
		result += "?" + target.RawQuery
	}
	if target.Fragment != "" {
		result += "#" + target.EscapedFragment()
	}
	return result
}

func (proxy *appProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	realm, target, client, ok := proxy.resolve(request)
	if !ok {
		http.NotFound(response, request)
		return
	}
	started := time.Now()
	defer func() {
		if proxy.complete != nil {
			proxy.complete(realm, time.Since(started))
		}
	}()

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(response, "read proxied request", http.StatusBadRequest)
		return
	}
	proxy.mu.Lock()
	proxy.nextID++
	id := fmt.Sprintf("request-%d", proxy.nextID)
	proxy.mu.Unlock()
	inspection := AppHTTPRequest{
		ID: id, Realm: realm, URL: target.String(), Method: request.Method,
		Headers: flattenHeaders(request.Header), Body: string(body),
	}
	decision := AppHTTPResponse{Action: "continue"}
	if proxy.decide != nil {
		decision, err = proxy.decide(request.Context(), inspection)
		if err != nil {
			http.Error(response, err.Error(), http.StatusBadGateway)
			return
		}
	}

	switch decision.Action {
	case "abort":
		reason := decision.Body
		if reason == "" {
			reason = "request aborted by Rush route"
		}
		http.Error(response, reason, http.StatusBadGateway)
		return
	case "fulfill":
		status := decision.Status
		if status == 0 {
			status = http.StatusOK
		}
		copyFlatHeaders(response.Header(), decision.Headers)
		response.WriteHeader(status)
		_, _ = io.WriteString(response, decision.Body)
		return
	case "", "continue":
	default:
		http.Error(response, "unknown Rush route action", http.StatusBadGateway)
		return
	}

	if decision.URL != "" {
		target, err = url.Parse(decision.URL)
		if err != nil || target.Scheme == "" || target.Host == "" {
			http.Error(response, "invalid continued request URL", http.StatusBadGateway)
			return
		}
	}
	method := request.Method
	if decision.Method != "" {
		method = decision.Method
	}
	if decision.Body != "" {
		body = []byte(decision.Body)
	}
	outbound, err := http.NewRequestWithContext(request.Context(), method, target.String(), strings.NewReader(string(body)))
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadGateway)
		return
	}
	copyHeaders(outbound.Header, request.Header)
	copyFlatHeaders(outbound.Header, decision.Headers)
	removeHopHeaders(outbound.Header)
	if outbound.Header.Get("Origin") != "" {
		outbound.Header.Set("Origin", target.Scheme+"://"+target.Host)
	}
	outbound.Host = target.Host

	upstream, err := client.Do(outbound)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadGateway)
		return
	}
	defer upstream.Body.Close()
	copyHeaders(response.Header(), upstream.Header)
	removeHopHeaders(response.Header())
	removeFrameAncestors(response.Header(), "Content-Security-Policy")
	removeFrameAncestors(response.Header(), "Content-Security-Policy-Report-Only")
	response.Header().Del("X-Frame-Options")
	rewriteResponseCookies(response.Header())
	if location := response.Header().Get("Location"); location != "" {
		if redirected, resolveErr := target.Parse(location); resolveErr == nil {
			response.Header().Set("Location", proxy.proxyURL(redirected))
		}
	}
	response.WriteHeader(upstream.StatusCode)
	_, _ = io.Copy(response, upstream.Body)
}

func (proxy *appProxy) resolve(request *http.Request) (string, *url.URL, *http.Client, bool) {
	proxy.mu.RLock()
	realm := proxy.active
	registered := proxy.realms[realm]
	proxy.mu.RUnlock()
	if registered == nil || registered.base == nil {
		return "", nil, nil, false
	}
	target := *registered.base
	target.Path = request.URL.Path
	target.RawPath = request.URL.RawPath
	target.RawQuery = request.URL.RawQuery
	return realm, &target, registered.client, true
}

func flattenHeaders(headers http.Header) map[string]string {
	result := make(map[string]string, len(headers))
	for name, values := range headers {
		result[name] = strings.Join(values, ", ")
	}
	return result
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func copyFlatHeaders(destination http.Header, source map[string]string) {
	for name, value := range source {
		destination.Set(name, value)
	}
}

func removeHopHeaders(headers http.Header) {
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		headers.Del(name)
	}
}

func rewriteResponseCookies(headers http.Header) {
	cookies := headers.Values("Set-Cookie")
	if len(cookies) == 0 {
		return
	}
	headers.Del("Set-Cookie")
	for _, cookie := range cookies {
		parts := strings.Split(cookie, ";")
		kept := parts[:1]
		for _, attribute := range parts[1:] {
			name := strings.ToLower(strings.TrimSpace(strings.SplitN(attribute, "=", 2)[0]))
			if name != "domain" && name != "path" && name != "secure" {
				kept = append(kept, attribute)
			}
		}
		kept = append(kept, " Path=/")
		headers.Add("Set-Cookie", strings.Join(kept, ";"))
	}
}

func removeFrameAncestors(headers http.Header, name string) {
	values := headers.Values(name)
	if len(values) == 0 {
		return
	}
	headers.Del(name)
	for _, value := range values {
		directives := strings.Split(value, ";")
		kept := directives[:0]
		for _, directive := range directives {
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(directive)), "frame-ancestors") {
				kept = append(kept, directive)
			}
		}
		if policy := strings.TrimSpace(strings.Join(kept, ";")); policy != "" {
			headers.Add(name, policy)
		}
	}
}

func marshalAppRequest(request AppHTTPRequest) string {
	payload, _ := json.Marshal(request)
	return string(payload)
}
