package rush

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppProxyNavigatesInspectsAndFulfillsRequests(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Upstream", "yes")
		_, _ = io.WriteString(response, request.Method+" "+request.URL.RequestURI())
	}))
	defer upstream.Close()

	var inspected []AppHTTPRequest
	proxy := newAppProxy("")
	proxy.decide = func(_ context.Context, request AppHTTPRequest) (AppHTTPResponse, error) {
		inspected = append(inspected, request)
		if strings.HasSuffix(request.URL, "/api/user") {
			return AppHTTPResponse{Action: "fulfill", Status: 201, Headers: map[string]string{"Content-Type": "application/json"}, Body: `{"name":"Ada"}`}, nil
		}
		return AppHTTPResponse{Action: "continue"}, nil
	}
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxy.origin = server.URL

	pageURL, err := proxy.navigate("test-1", upstream.URL+"/account?tab=profile")
	if err != nil {
		t.Fatal(err)
	}
	pageResponse, err := http.Get(pageURL)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, _ := io.ReadAll(pageResponse.Body)
	pageResponse.Body.Close()
	if got := string(pageBody); got != "GET /account?tab=profile" {
		t.Fatalf("page body = %q", got)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/user", strings.NewReader("query"))
	request.Header.Set("Referer", pageURL)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 201 || string(body) != `{"name":"Ada"}` {
		t.Fatalf("mock response = %d %q", response.StatusCode, body)
	}
	if len(inspected) != 2 || inspected[1].Method != http.MethodPost || inspected[1].Body != "query" || inspected[1].URL != upstream.URL+"/api/user" {
		t.Fatalf("inspected = %#v", inspected)
	}
}

func TestAppProxyRewritesRedirectsAndRemovesFramingRestrictions(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'")
		response.Header().Set("X-Frame-Options", "DENY")
		http.Redirect(response, request, "/signed-in", http.StatusFound)
	}))
	defer upstream.Close()
	proxy := newAppProxy("")
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxy.origin = server.URL
	pageURL, _ := proxy.navigate("test-2", upstream.URL+"/login")

	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(pageURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if got := response.Header.Get("Location"); got != server.URL+appProxyPrefix+"test-2/signed-in" {
		t.Fatalf("location = %q", got)
	}
	if response.Header.Get("Content-Security-Policy") != "default-src 'self'" || response.Header.Get("X-Frame-Options") != "" {
		t.Fatalf("framing headers were not removed: %#v", response.Header)
	}
}

func TestAppProxyPartitionsServerCookiesByTestRealm(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/set" {
			http.SetCookie(response, &http.Cookie{Name: "session", Value: "alice", Path: "/", HttpOnly: true})
			return
		}
		_, _ = io.WriteString(response, request.Header.Get("Cookie"))
	}))
	defer upstream.Close()
	proxy := newAppProxy("")
	server := httptest.NewServer(proxy)
	defer server.Close()
	proxy.origin = server.URL

	firstSet, _ := proxy.navigate("first", upstream.URL+"/set")
	response, err := http.Get(firstSet)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	firstWho, _ := proxy.navigate("first", upstream.URL+"/who")
	response, _ = http.Get(firstWho)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if !strings.Contains(string(body), "session=alice") {
		t.Fatalf("first realm cookie = %q", body)
	}

	secondWho, _ := proxy.navigate("second", upstream.URL+"/who")
	response, _ = http.Get(secondWho)
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "" {
		t.Fatalf("second realm inherited cookie %q", body)
	}
}
