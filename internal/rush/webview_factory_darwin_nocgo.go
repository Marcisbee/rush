//go:build darwin && !cgo

package rush

import (
	"errors"

	webview "github.com/moxcomic/go-webview"
)

func newWebView(bool) (webview.WebView, error) {
	return nil, errors.New("the macOS WKWebView adapter requires cgo; rebuild with CGO_ENABLED=1")
}
