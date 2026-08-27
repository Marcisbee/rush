package rush

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserBundleSourceStoreServesAndDeletesBatchSources(t *testing.T) {
	store := newBrowserBundleSourceStore()
	store.Put("run-1", map[int]browserBundleSource{2: {hash: "hash-2", source: "globalThis.answer = 42"}})

	request := httptest.NewRequest(http.MethodGet, "/__rush/bundle/run-1/2", nil)
	response := httptest.NewRecorder()
	store.ServeHTTP(response, request)
	result := response.Result()
	defer result.Body.Close()
	contents, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	want := "window.__rush.installBundle(\"hash-2\",function(){\nglobalThis.answer = 42\n});\n"
	if result.StatusCode != http.StatusOK || string(contents) != want {
		t.Fatalf("bundle response = %d %q", result.StatusCode, contents)
	}
	if result.Header.Get("Cache-Control") != "no-store" || result.Header.Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("bundle headers = %#v", result.Header)
	}

	store.Delete("run-1")
	missing := httptest.NewRecorder()
	store.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/__rush/bundle/run-1/2", nil))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("deleted bundle status = %d, want %d", missing.Code, http.StatusNotFound)
	}
}

func TestBrowserBundleSourceStoreRejectsInvalidRequests(t *testing.T) {
	store := newBrowserBundleSourceStore()
	store.Put("run-1", map[int]browserBundleSource{0: {hash: "hash-0", source: "source"}})

	for _, target := range []string{
		"/__rush/bundle/run-1",
		"/__rush/bundle/run-1/not-an-index",
		"/__rush/bundle/unknown/0",
		"/__rush/bundle/run-1/1",
	} {
		response := httptest.NewRecorder()
		store.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", target, response.Code, http.StatusNotFound)
		}
	}

	response := httptest.NewRecorder()
	store.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/__rush/bundle/run-1/0", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
