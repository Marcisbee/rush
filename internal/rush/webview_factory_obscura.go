//go:build linux && rush_obscura

package rush

import (
	"errors"

	webview "github.com/moxcomic/go-webview"
)

func newWebView(debug bool) (webview.WebView, error) {
	if debug {
		return nil, errors.New("the Obscura adapter is headless-only")
	}
	return newObscuraWebView()
}
