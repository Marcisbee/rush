//go:build rush_lightpanda && linux

package rush

import (
	"github.com/Marcisbee/rush/internal/lightpanda"
	webview "github.com/moxcomic/go-webview"
)

func newWebView(debug bool) (webview.WebView, error) { return lightpanda.New(debug) }
