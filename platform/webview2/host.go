package webview2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
)

var safeArtifactName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type nativeDriver interface {
	start(context.Context, Config, string, func(json.RawMessage)) error
	navigate(context.Context, string) error
	evaluate(context.Context, string) (json.RawMessage, error)
	capturePNG(context.Context) ([]byte, error)
	trustedMouse(context.Context, MouseAction) error
	trustedKey(context.Context, KeyAction) error
	close() error
}

type driverFactory func() nativeDriver

// Host owns the persistent native WebView2 processes and a reusable realm pool.
type Host struct {
	config  Config
	ctx     context.Context
	cancel  context.CancelFunc
	batcher *bridgeBatcher
	factory driverFactory

	mu        sync.Mutex
	realms    []*Realm
	wait      chan struct{}
	created   uint64
	reused    uint64
	closed    bool
	closeErr  error
	closeDone chan struct{}
}

// Stats reports realm reuse without mixing it into user-test timing.
type Stats struct {
	CreatedRealms uint64
	ReusedRealms  uint64
}

// Realm is a persistent WebView2 controller leased from a Host.
type Realm struct {
	id         string
	host       *Host
	driver     nativeDriver
	inUse      bool
	generation uint64
	closed     bool
}

// FailureArtifacts are the screenshot and DOM snapshot captured for a failure.
type FailureArtifacts struct {
	ScreenshotPath string
	DOMPath        string
}

// New creates a persistent WebView2 host. The native runtime is initialized
// lazily when the first realm is acquired.
func New(config Config) (*Host, error) {
	return newHost(config, newPlatformDriver)
}

func newHost(config Config, factory driverFactory) (*Host, error) {
	config, err := config.normalized()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	host := &Host{config: config, ctx: ctx, cancel: cancel, factory: factory, wait: make(chan struct{}, 1), closeDone: make(chan struct{})}
	host.batcher = newBridgeBatcher(ctx, config.BatchMaxMessages, config.BatchMaxBytes, config.BatchFlushInterval)
	return host, nil
}

// Batches returns the adapter's ordered, batched page-to-host message stream.
func (h *Host) Batches() <-chan BridgeBatch { return h.batcher.out }

// Acquire leases a warm realm, creating one only while the configured pool has
// capacity. The same native controller survives across lease generations.
func (h *Host) Acquire(ctx context.Context) (*Realm, error) {
	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errors.New("webview2: host is closed")
		}
		for _, realm := range h.realms {
			if !realm.inUse && !realm.closed {
				realm.inUse = true
				atomic.AddUint64(&realm.generation, 1)
				h.reused++
				h.mu.Unlock()
				return realm, nil
			}
		}
		if len(h.realms) < h.config.RealmPoolSize {
			id := fmt.Sprintf("webview2-%d", len(h.realms)+1)
			realm := &Realm{id: id, host: h, driver: h.factory(), inUse: true, generation: 1}
			h.realms = append(h.realms, realm)
			h.created++
			h.mu.Unlock()
			err := realm.driver.start(ctx, h.config, id, func(payload json.RawMessage) {
				_ = h.batcher.push(h.ctx, BridgeMessage{RealmID: id, Payload: append(json.RawMessage(nil), payload...)})
			})
			if err != nil {
				_ = realm.driver.close()
				h.mu.Lock()
				realm.closed = true
				realm.inUse = false
				for index, candidate := range h.realms {
					if candidate == realm {
						h.realms = append(h.realms[:index], h.realms[index+1:]...)
						h.created--
						break
					}
				}
				h.mu.Unlock()
				select {
				case h.wait <- struct{}{}:
				default:
				}
				return nil, err
			}
			return realm, nil
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-h.ctx.Done():
			return nil, errors.New("webview2: host is closed")
		case <-h.wait:
		}
	}
}

func (h *Host) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Stats{CreatedRealms: h.created, ReusedRealms: h.reused}
}

// ID is stable for the lifetime of the native realm.
func (r *Realm) ID() string { return r.id }

// Generation increments each time the persistent realm is leased.
func (r *Realm) Generation() uint64 { return atomic.LoadUint64(&r.generation) }

// LoadHTML navigates the leased WebView2 controller to an HTML document.
func (r *Realm) LoadHTML(ctx context.Context, html string) error {
	if err := r.usable(); err != nil {
		return err
	}
	return r.driver.navigate(ctx, html)
}

// Evaluate executes JavaScript in the real WebView2 page and returns its JSON value.
func (r *Realm) Evaluate(ctx context.Context, script string) (json.RawMessage, error) {
	if err := r.usable(); err != nil {
		return nil, err
	}
	return r.driver.evaluate(ctx, script)
}

func (r *Realm) TrustedMouse(ctx context.Context, action MouseAction) error {
	if err := r.usable(); err != nil {
		return err
	}
	return r.driver.trustedMouse(ctx, action)
}

func (r *Realm) TrustedKey(ctx context.Context, action KeyAction) error {
	if err := r.usable(); err != nil {
		return err
	}
	return r.driver.trustedKey(ctx, action)
}

// CaptureFailure writes WebView2's rendered PNG and the current DOM snapshot.
func (r *Realm) CaptureFailure(ctx context.Context, testName string) (FailureArtifacts, error) {
	if err := r.usable(); err != nil {
		return FailureArtifacts{}, err
	}
	name := safeArtifactName.ReplaceAllString(testName, "-")
	if name == "" {
		name = "failure"
	}
	dir := filepath.Join(r.host.config.ArtifactDir, r.id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return FailureArtifacts{}, err
	}
	png, pngErr := r.driver.capturePNG(ctx)
	dom, domErr := r.driver.evaluate(ctx, `document.documentElement.outerHTML`)
	artifacts := FailureArtifacts{
		ScreenshotPath: filepath.Join(dir, name+".png"),
		DOMPath:        filepath.Join(dir, name+".html"),
	}
	var errs []error
	if pngErr != nil {
		errs = append(errs, fmt.Errorf("screenshot: %w", pngErr))
	} else if err := os.WriteFile(artifacts.ScreenshotPath, png, 0o644); err != nil {
		errs = append(errs, err)
	}
	if domErr != nil {
		errs = append(errs, fmt.Errorf("DOM snapshot: %w", domErr))
	} else {
		var html string
		if err := json.Unmarshal(dom, &html); err != nil {
			errs = append(errs, err)
		} else if err := os.WriteFile(artifacts.DOMPath, []byte(html), 0o644); err != nil {
			errs = append(errs, err)
		}
	}
	return artifacts, errors.Join(errs...)
}

// Release resets page-owned state and returns the native realm to the pool.
func (r *Realm) Release(ctx context.Context) error {
	if err := r.usable(); err != nil {
		return err
	}
	resetErr := r.driver.navigate(ctx, resetDocument)
	r.host.mu.Lock()
	r.inUse = false
	if resetErr != nil {
		// A realm that could not reach the reset document may still contain
		// page-owned globals, listeners, or timers. Remove it from the pool so a
		// later lease can never observe that dirty state.
		r.closed = true
		for index, candidate := range r.host.realms {
			if candidate == r {
				r.host.realms = append(r.host.realms[:index], r.host.realms[index+1:]...)
				break
			}
		}
	}
	r.host.mu.Unlock()
	select {
	case r.host.wait <- struct{}{}:
	default:
	}
	if resetErr != nil {
		return errors.Join(resetErr, r.driver.close())
	}
	return nil
}

func (r *Realm) usable() error {
	r.host.mu.Lock()
	defer r.host.mu.Unlock()
	if r.closed || r.host.closed {
		return errors.New("webview2: realm is closed")
	}
	if !r.inUse {
		return errors.New("webview2: realm is not leased")
	}
	return nil
}

func (h *Host) Close() error {
	h.mu.Lock()
	if h.closed {
		done := h.closeDone
		h.mu.Unlock()
		<-done
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.closeErr
	}
	h.closed = true
	realms := append([]*Realm(nil), h.realms...)
	for _, realm := range realms {
		realm.closed = true
	}
	h.mu.Unlock()
	h.cancel()
	var errs []error
	for _, realm := range realms {
		if err := realm.driver.close(); err != nil {
			errs = append(errs, err)
		}
	}
	h.batcher.close()
	h.mu.Lock()
	h.closeErr = errors.Join(errs...)
	close(h.closeDone)
	err := h.closeErr
	h.mu.Unlock()
	return err
}

const resetDocument = `<!doctype html><html><head><meta charset="utf-8"></head><body></body></html>`
