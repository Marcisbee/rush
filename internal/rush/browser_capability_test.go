package rush

import (
	"errors"
	"runtime"
	"testing"
)

func TestResolveNativeInputCapability(t *testing.T) {
	available := resolveNativeInputCapability(true, nil)
	if !available.Available || available.Reason != "" {
		t.Fatalf("headed capability = %#v; want available", available)
	}

	unavailable := resolveNativeInputCapability(true, errors.New("adapter unavailable"))
	if unavailable.Available || unavailable.Reason != "adapter unavailable" {
		t.Fatalf("adapter error capability = %#v; want unavailable reason", unavailable)
	}

	hidden := resolveNativeInputCapability(false, nil)
	if runtime.GOOS == "darwin" {
		if hidden.Available || hidden.Reason != "trusted native input requires --headed on macOS" {
			t.Fatalf("hidden macOS capability = %#v; want headed requirement", hidden)
		}
	} else if !hidden.Available {
		t.Fatalf("hidden %s capability = %#v; want available", runtime.GOOS, hidden)
	}
}
