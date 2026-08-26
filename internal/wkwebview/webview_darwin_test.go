//go:build darwin && cgo

package wkwebview

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testView *View

func TestMain(m *testing.M) {
	native, err := New(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testView = native.(*View)
	result := make(chan int, 1)
	go func() {
		result <- m.Run()
		testView.Terminate()
	}()
	testView.Run()
	code := <-result
	testView.Destroy()
	os.Exit(code)
}

func TestHiddenWKWebViewBridgeEvaluationReuseAndFailureArtifacts(t *testing.T) {
	ready := make(chan struct{}, 1)
	if err := testView.Bind("__rushAdapterReady", func() { ready <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	testView.SetHtml(`<!doctype html><html><body><main data-rush-failure>broken state</main><script>window.__rushAdapterReady()</script></body></html>`)
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("WKWebView bridge did not become ready")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := testView.Evaluate(ctx, `globalThis.__rushWarmMarker = 41`); err != nil {
		t.Fatal(err)
	}
	raw, err := testView.Evaluate(ctx, `globalThis.__rushWarmMarker + 1`)
	if err != nil {
		t.Fatal(err)
	}
	var marker int
	if err := json.Unmarshal(raw, &marker); err != nil {
		t.Fatal(err)
	}
	if marker != 42 {
		t.Fatalf("warm marker = %d, want 42", marker)
	}
	artifacts, err := testView.CaptureFailure(ctx, t.TempDir(), "failed: test / one")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(artifacts.DOMPath); err != nil || !strings.Contains(string(data), "data-rush-failure") {
		t.Fatalf("DOM artifact %s: %q, %v", artifacts.DOMPath, data, err)
	}
	if info, err := os.Stat(artifacts.ScreenshotPath); err != nil || info.Size() == 0 || filepath.Ext(artifacts.ScreenshotPath) != ".png" {
		t.Fatalf("screenshot artifact %s: %v", artifacts.ScreenshotPath, err)
	}
}
