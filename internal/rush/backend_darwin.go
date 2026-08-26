//go:build darwin

package rush

import (
	"errors"
	"fmt"
	"io"
	"runtime"
)

func BackendName() string  { return "WKWebView" }
func SupportsHeaded() bool { return true }

func backendSocketMode(headed bool) string {
	if headed {
		return "wkwebview-headed"
	}
	return "wkwebview-hidden"
}

func prepareBrowser(bool) (func(), error) { return func() {}, nil }

func Doctor(output io.Writer) error {
	if !cgoEnabled {
		return errors.New("the macOS WKWebView adapter requires cgo; rebuild with CGO_ENABLED=1")
	}
	fmt.Fprintln(output, "Go host: available")
	fmt.Fprintf(output, "WKWebView: Apple system framework on %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintln(output, "hidden execution: available; use --headed for a visible Web Inspector window")
	return nil
}
