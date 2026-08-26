//go:build !darwin

package rush

import webview "github.com/moxcomic/go-webview"

func newWebView(debug bool) (webview.WebView, error) { return webview.New(debug) }
