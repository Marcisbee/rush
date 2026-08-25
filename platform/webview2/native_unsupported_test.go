//go:build !windows

package webview2

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsClearly(t *testing.T) {
	host, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	_, err = host.Acquire(context.Background())
	if !errors.Is(err, ErrWindowsRequired) {
		t.Fatalf("Acquire error = %v", err)
	}
}
