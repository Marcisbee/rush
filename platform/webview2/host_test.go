package webview2

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type fakeDriver struct {
	mu          sync.Mutex
	navigated   []string
	onMessage   func(json.RawMessage)
	closed      bool
	evaluation  json.RawMessage
	startErr    error
	navigateErr error
}

func (d *fakeDriver) start(_ context.Context, _ Config, _ string, onMessage func(json.RawMessage)) error {
	d.onMessage = onMessage
	return d.startErr
}
func (d *fakeDriver) navigate(_ context.Context, html string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.navigated = append(d.navigated, html)
	return d.navigateErr
}
func (d *fakeDriver) evaluate(context.Context, string) (json.RawMessage, error) {
	if d.evaluation == nil {
		return json.RawMessage(`"<html>failure</html>"`), nil
	}
	return d.evaluation, nil
}
func (*fakeDriver) capturePNG(context.Context) ([]byte, error)      { return []byte("png"), nil }
func (*fakeDriver) trustedMouse(context.Context, MouseAction) error { return nil }
func (*fakeDriver) trustedKey(context.Context, KeyAction) error     { return nil }
func (d *fakeDriver) close() error                                  { d.closed = true; return nil }

func TestHostReusesAndResetsRealms(t *testing.T) {
	var drivers []*fakeDriver
	host, err := newHost(Config{RealmPoolSize: 1}, func() nativeDriver {
		driver := &fakeDriver{}
		drivers = append(drivers, driver)
		return driver
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })

	realm, err := host.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := realm.LoadHTML(context.Background(), "<button>test</button>"); err != nil {
		t.Fatal(err)
	}
	if err := realm.Release(context.Background()); err != nil {
		t.Fatal(err)
	}
	reused, err := host.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reused != realm {
		t.Fatal("expected the native realm to be reused")
	}
	if reused.Generation() != 2 {
		t.Fatalf("generation = %d, want 2", reused.Generation())
	}
	stats := host.Stats()
	if stats.CreatedRealms != 1 || stats.ReusedRealms != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if got := drivers[0].navigated; len(got) != 2 || got[1] != resetDocument {
		t.Fatalf("navigations = %#v", got)
	}
}

func TestAcquireHonorsPoolLimitAndContext(t *testing.T) {
	host, err := newHost(Config{RealmPoolSize: 1}, func() nativeDriver { return &fakeDriver{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	if _, err := host.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := host.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire error = %v", err)
	}
}

func TestFailedStartDoesNotConsumePoolCapacity(t *testing.T) {
	attempt := 0
	host, err := newHost(Config{RealmPoolSize: 1}, func() nativeDriver {
		attempt++
		if attempt == 1 {
			return &fakeDriver{startErr: errors.New("runtime unavailable")}
		}
		return &fakeDriver{}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	if _, err := host.Acquire(context.Background()); err == nil {
		t.Fatal("expected first start to fail")
	}
	if _, err := host.Acquire(context.Background()); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if stats := host.Stats(); stats.CreatedRealms != 1 {
		t.Fatalf("stats after retry = %+v", stats)
	}
}

func TestFailedResetDiscardsDirtyRealm(t *testing.T) {
	var drivers []*fakeDriver
	host, err := newHost(Config{RealmPoolSize: 1}, func() nativeDriver {
		driver := &fakeDriver{}
		drivers = append(drivers, driver)
		return driver
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })

	realm, err := host.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	drivers[0].navigateErr = errors.New("renderer stopped")
	if err := realm.Release(context.Background()); err == nil {
		t.Fatal("expected reset failure")
	}
	if !drivers[0].closed {
		t.Fatal("failed realm was not closed")
	}

	replacement, err := host.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if replacement == realm || len(drivers) != 2 {
		t.Fatal("expected a fresh realm after reset failure")
	}
}

func TestConcurrentCloseWaitsForOneResult(t *testing.T) {
	host, err := newHost(Config{}, func() nativeDriver { return &fakeDriver{} })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	go func() { results <- host.Close() }()
	go func() { results <- host.Close() }()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBridgeBatchesByCountAndPreservesOrder(t *testing.T) {
	var driver *fakeDriver
	host, err := newHost(Config{BatchMaxMessages: 2, BatchFlushInterval: time.Hour}, func() nativeDriver { driver = &fakeDriver{}; return driver })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	if _, err := host.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	driver.onMessage(json.RawMessage(`{"n":1}`))
	driver.onMessage(json.RawMessage(`{"n":2}`))
	select {
	case batch := <-host.Batches():
		if batch.Sequence != 1 || len(batch.Messages) != 2 {
			t.Fatalf("unexpected batch: %+v", batch)
		}
		if string(batch.Messages[0].Payload) != `{"n":1}` || string(batch.Messages[1].Payload) != `{"n":2}` {
			t.Fatalf("messages reordered: %+v", batch.Messages)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bridge batch")
	}
}

func TestCaptureFailureWritesPNGAndDOM(t *testing.T) {
	dir := t.TempDir()
	host, err := newHost(Config{ArtifactDir: dir}, func() nativeDriver { return &fakeDriver{} })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	realm, err := host.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := realm.CaptureFailure(context.Background(), "suite / unsafe name")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(artifacts.ScreenshotPath) != "suite-unsafe-name.png" {
		t.Fatalf("unexpected screenshot name: %s", artifacts.ScreenshotPath)
	}
	if got, err := os.ReadFile(artifacts.ScreenshotPath); err != nil || string(got) != "png" {
		t.Fatalf("screenshot = %q, %v", got, err)
	}
	if got, err := os.ReadFile(artifacts.DOMPath); err != nil || string(got) != "<html>failure</html>" {
		t.Fatalf("DOM = %q, %v", got, err)
	}
}
