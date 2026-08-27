//go:build rush_lightpanda && linux

package lightpanda

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestSynchronizedBufferRetainsBoundedTail(t *testing.T) {
	var buffer synchronizedBuffer
	prefix := bytes.Repeat([]byte("a"), processOutputLimit-10)
	if written, err := buffer.Write(prefix); err != nil || written != len(prefix) {
		t.Fatalf("write prefix = %d, %v", written, err)
	}
	if written, err := buffer.Write([]byte("0123456789abcdefghij")); err != nil || written != 20 {
		t.Fatalf("write suffix = %d, %v", written, err)
	}
	value := buffer.String()
	if len(value) != processOutputLimit || value[len(value)-20:] != "0123456789abcdefghij" {
		t.Fatalf("buffer retained %d bytes with tail %q", len(value), value[len(value)-20:])
	}
}

func TestBindingEvaluationStorageAndFetchRoundTrip(t *testing.T) {
	if os.Getenv("RUSH_LIGHTPANDA_PATH") == "" {
		t.Skip("RUSH_LIGHTPANDA_PATH is not configured")
	}
	reported := make(chan string, 1)
	view, err := New(false)
	if err != nil {
		t.Fatal(err)
	}
	defer view.Destroy()
	if err := view.Bind("add", func(left, right int) int { return left + right }); err != nil {
		t.Fatal(err)
	}
	if err := view.Bind("report", func(value string) { reported <- value }); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`<script>add(2, 3).then(value => report(String(value)))</script>`))
	}))
	defer server.Close()
	view.Navigate(server.URL)
	select {
	case value := <-reported:
		if value != "5" {
			t.Fatalf("reported %q, want 5", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("binding round trip timed out")
	}
	view.Eval(`report("eval")`)
	select {
	case value := <-reported:
		if value != "eval" {
			t.Fatalf("reported %q, want eval", value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation binding round trip timed out")
	}
	view.Eval(`(async () => {
      await report("storage-start");
      try { localStorage.clear(); } catch (_) {}
      try { sessionStorage.clear(); } catch (_) {}
      await report("storage-basic");
      try { if (indexedDB.databases) await indexedDB.databases(); } catch (_) {}
      await report("storage-indexeddb");
      try { await caches.keys(); } catch (_) {}
      await report("storage-caches");
      try { if (navigator.serviceWorker) await navigator.serviceWorker.getRegistrations(); } catch (_) {}
      await report("storage-done");
    })()`)
	for _, expected := range []string{"storage-start", "storage-basic", "storage-indexeddb", "storage-caches", "storage-done"} {
		select {
		case value := <-reported:
			if value != expected {
				t.Fatalf("reported %q, want %q", value, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %q", expected)
		}
	}
	view.Eval(`fetch(location.href).then(response => response.text()).then(text => report(String(text.length)))`)
	select {
	case value := <-reported:
		if value == "0" {
			t.Fatal("fetch returned an empty response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fetch timed out")
	}
}
