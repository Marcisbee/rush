//go:build rush_wpe

package rush

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BackendName identifies the browser engine host compiled into this binary.
func BackendName() string { return "WPE WebKit" }

// SupportsHeaded reports whether this adapter can present a debugging window.
func SupportsHeaded() bool { return false }

func prepareBrowser(headed bool) (func(), error) {
	if headed {
		return nil, errors.New("the WPE adapter is headless-only; use the default WebKitGTK build for headed debugging")
	}
	bridge, err := wpeBridgePath()
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("WEBVIEW_PATH", filepath.Dir(bridge)); err != nil {
		return nil, fmt.Errorf("select WPE adapter bridge: %w", err)
	}
	return func() {}, nil
}

func wpeBridgePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	bridge := filepath.Join(filepath.Dir(executable), "libwebview.so")
	if _, err := os.Stat(bridge); err != nil {
		return "", fmt.Errorf("WPE adapter bridge was not found beside the executable at %s; run make build-wpe", bridge)
	}
	return bridge, nil
}

// Doctor checks prerequisites that can be validated without starting WebKit.
func Doctor(output io.Writer) error {
	if _, err := wpeBridgePath(); err != nil {
		return err
	}
	fmt.Fprintln(output, "Go host: available")
	fmt.Fprintln(output, "headless display: WPE headless platform (no X11 or Wayland compositor required)")
	fmt.Fprintln(output, "WPE WebKit: checked when the native host starts (run `rush test benchmarks/fixtures/assertions.ts`)")
	return nil
}
