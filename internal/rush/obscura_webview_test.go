//go:build linux && rush_obscura

package rush

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestObscuraBindingDecodesArgumentsAndReturnsErrors(t *testing.T) {
	binding, err := makeObscuraBinding(func(prefix string, values ...int) (string, error) {
		if prefix == "fail" {
			return "", errors.New("requested failure")
		}
		return prefix + ":" + strings.Trim(string(mustJSON(values)), "[]"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := binding(json.RawMessage(`["value",2,3]`))
	if err != nil {
		t.Fatal(err)
	}
	if result != "value:2,3" {
		t.Fatalf("binding result = %v, want value:2,3", result)
	}
	if _, err := binding(json.RawMessage(`["fail"]`)); err == nil || err.Error() != "requested failure" {
		t.Fatalf("binding error = %v", err)
	}
}

func TestObscuraBindingRejectsInvalidSignaturesAndArguments(t *testing.T) {
	if _, err := makeObscuraBinding("not a function"); err == nil {
		t.Fatal("non-function binding was accepted")
	}
	if _, err := makeObscuraBinding(func() (int, int) { return 0, 0 }); err == nil {
		t.Fatal("binding with a non-error second return was accepted")
	}
	binding, err := makeObscuraBinding(func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding(json.RawMessage(`[]`)); err == nil {
		t.Fatal("binding accepted a missing argument")
	}
}

func TestObscuraBackendIsHeadlessOnly(t *testing.T) {
	if SupportsHeaded() {
		t.Fatal("Obscura adapter reported headed support")
	}
	if _, err := prepareBrowser(true); err == nil || !strings.Contains(err.Error(), "headless-only") {
		t.Fatalf("headed Obscura error = %v", err)
	}
}

func TestObscuraWebViewBridgeSmoke(t *testing.T) {
	if os.Getenv("OBSCURA_BIN") == "" {
		t.Skip("OBSCURA_BIN is not configured")
	}
	view, err := newObscuraWebView()
	if err != nil {
		t.Fatal(err)
	}
	defer view.Destroy()
	reported := make(chan string, 1)
	if err := view.Bind("__rushReport", func(value string) { reported <- value }); err != nil {
		t.Fatal(err)
	}
	view.Navigate("data:text/html,<main>Obscura</main>")
	view.Eval(`new Promise(resolve => setTimeout(() => {
      __rushReport(document.createElement("button").tagName);
      resolve();
    }, 20))`)
	select {
	case value := <-reported:
		if value != "BUTTON" {
			t.Fatalf("bridge value = %q, want BUTTON", value)
		}
		if view.pumping.Load() {
			t.Fatal("Obscura task pump remained active after the batch report")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Obscura binding did not report from the page")
	}
}

func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
