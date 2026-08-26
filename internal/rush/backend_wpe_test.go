//go:build rush_wpe

package rush

import (
	"strings"
	"testing"
)

func TestWPEBackendIsHeadlessOnly(t *testing.T) {
	if SupportsHeaded() {
		t.Fatal("WPE headless adapter reported headed support")
	}
	if _, err := prepareBrowser(true); err == nil || !strings.Contains(err.Error(), "headless-only") {
		t.Fatalf("headed WPE mode error = %v", err)
	}
}
