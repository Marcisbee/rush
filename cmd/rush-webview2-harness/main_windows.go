//go:build windows

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Marcisbee/rush/harness"
	"github.com/Marcisbee/rush/platform/webview2"
)

func main() {
	debug := flag.Bool("debug", false, "show WebView2 and open DevTools")
	repeats := flag.Int("repeats", 10, "warm performance repeat count")
	artifacts := flag.String("artifacts", ".rush/harness-artifacts", "failure artifact directory")
	flag.Parse()
	mode := webview2.ModeHidden
	if *debug {
		mode = webview2.ModeDebug
	}
	host, err := webview2.New(webview2.Config{Mode: mode, ArtifactDir: *artifacts, BatchMaxMessages: 8})
	fatalIf(err)
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	realm, err := host.Acquire(ctx)
	fatalIf(err)

	progress("conformance")
	conformance, err := harness.RunConformance(ctx, realm)
	fatalIf(err)
	progress("performance")
	performance, err := harness.RunPerformance(ctx, realm, *repeats)
	fatalIf(err)
	progress("trusted input")
	trusted, err := runTrustedInput(ctx, realm)
	fatalIf(err)
	progress("bridge batching")
	bridge, err := runBridge(ctx, realm, host.Batches())
	fatalIf(err)
	progress("failure artifacts")
	artifactsResult, err := realm.CaptureFailure(ctx, "harness-sample")
	fatalIf(err)
	progress("realm reuse")
	firstID := realm.ID()
	fatalIf(realm.Release(ctx))
	reused, err := host.Acquire(ctx)
	fatalIf(err)
	reusePassed := reused.ID() == firstID && reused.Generation() == 2
	fatalIf(reused.Release(ctx))

	result := struct {
		Conformance    harness.Conformance       `json:"conformance"`
		Performance    harness.Performance       `json:"performance"`
		TrustedInput   trustedResult             `json:"trustedInput"`
		BridgeMessages int                       `json:"bridgeMessages"`
		RealmReuse     bool                      `json:"realmReuse"`
		Stats          webview2.Stats            `json:"stats"`
		Artifacts      webview2.FailureArtifacts `json:"artifacts"`
	}{conformance, performance, trusted, bridge, reusePassed, host.Stats(), artifactsResult}
	encoded, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(encoded))
	if !trusted.Passed() || bridge != 10 || !reusePassed || !performance.Assertions.Passed || !performance.DOM.Passed {
		os.Exit(1)
	}
}

type trustedResult struct {
	ClickTrusted string `json:"clickTrusted"`
	KeyTrusted   string `json:"keyTrusted"`
	Value        string `json:"value"`
}

func (r trustedResult) Passed() bool {
	return r.ClickTrusted == "true" && r.KeyTrusted == "true" && r.Value == "Rush"
}

func runTrustedInput(ctx context.Context, realm *webview2.Realm) (trustedResult, error) {
	err := realm.LoadHTML(ctx, `<!doctype html><style>body{margin:0}input{position:absolute;left:20px;top:20px;width:240px;height:40px}</style><input id="target"><script>target.addEventListener('click',event=>target.dataset.clickTrusted=String(event.isTrusted));target.addEventListener('beforeinput',event=>target.dataset.keyTrusted=String(event.isTrusted))</script>`)
	if err != nil {
		return trustedResult{}, err
	}
	if err := realm.TrustedMouse(ctx, webview2.MouseAction{X: 50, Y: 40, Button: webview2.MouseButtonLeft}); err != nil {
		return trustedResult{}, err
	}
	if err := realm.TrustedKey(ctx, webview2.KeyAction{Text: "Rush"}); err != nil {
		return trustedResult{}, err
	}
	raw, err := realm.Evaluate(ctx, `({click:target.dataset.clickTrusted,key:target.dataset.keyTrusted,value:target.value})`)
	if err != nil {
		return trustedResult{}, err
	}
	var result struct{ Click, Key, Value string }
	if err := json.Unmarshal(raw, &result); err != nil {
		return trustedResult{}, err
	}
	return trustedResult{ClickTrusted: result.Click, KeyTrusted: result.Key, Value: result.Value}, nil
}

func runBridge(ctx context.Context, realm *webview2.Realm, batches <-chan webview2.BridgeBatch) (int, error) {
	if _, err := realm.Evaluate(ctx, `(() => { for (let i=0;i<10;i++) chrome.webview.postMessage(JSON.stringify({kind:'harness',index:i})); return true })()`); err != nil {
		return 0, err
	}
	count := 0
	for count < 10 {
		select {
		case batch := <-batches:
			count += len(batch.Messages)
		case <-ctx.Done():
			return count, ctx.Err()
		}
	}
	return count, nil
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func progress(stage string) { fmt.Fprintln(os.Stderr, "harness:", stage) }
