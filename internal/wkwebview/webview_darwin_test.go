//go:build darwin && cgo

package wkwebview

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHiddenWKWebViewBridgeEvaluationReuseAndFailureArtifacts(t *testing.T) {
	native, err := New(false)
	if err != nil {
		t.Fatal(err)
	}
	view := native.(*View)
	defer view.Destroy()

	ready := make(chan struct{}, 1)
	if err := view.Bind("__rushAdapterReady", func() { ready <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	view.SetHtml(`<!doctype html><html><body><main data-rush-failure>broken state</main><script>window.__rushAdapterReady()</script></body></html>`)

	type result struct {
		artifacts FailureArtifacts
		marker    int
		err       error
	}
	finished := make(chan result, 1)
	go func() {
		select {
		case <-ready:
		case <-time.After(10 * time.Second):
			finished <- result{err: context.DeadlineExceeded}
			view.Terminate()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := view.Evaluate(ctx, `globalThis.__rushWarmMarker = 41`); err != nil {
			finished <- result{err: err}
			view.Terminate()
			return
		}
		raw, err := view.Evaluate(ctx, `globalThis.__rushWarmMarker + 1`)
		var marker int
		if err == nil {
			err = json.Unmarshal(raw, &marker)
		}
		directory := t.TempDir()
		artifacts, captureErr := view.CaptureFailure(ctx, directory, "failed: test / one")
		if err == nil {
			err = captureErr
		}
		finished <- result{artifacts: artifacts, marker: marker, err: err}
		view.Terminate()
	}()
	view.Run()

	got := <-finished
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.marker != 42 {
		t.Fatalf("warm marker = %d, want 42", got.marker)
	}
	if data, err := os.ReadFile(got.artifacts.DOMPath); err != nil || !strings.Contains(string(data), "data-rush-failure") {
		t.Fatalf("DOM artifact %s: %q, %v", got.artifacts.DOMPath, data, err)
	}
	if info, err := os.Stat(got.artifacts.ScreenshotPath); err != nil || info.Size() == 0 || filepath.Ext(got.artifacts.ScreenshotPath) != ".png" {
		t.Fatalf("screenshot artifact %s: %v", got.artifacts.ScreenshotPath, err)
	}
}
