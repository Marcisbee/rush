//go:build darwin && cgo

package rush

import (
	webview "github.com/moxcomic/go-webview"

	"github.com/Marcisbee/rush/internal/wkwebview"
)

func newWebView(debug bool) (webview.WebView, error) { return wkwebview.New(debug) }
