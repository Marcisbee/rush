//go:build rush_wpe

package rush

import (
	"path/filepath"
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

func TestWPEBackendUsesDedicatedDaemonSocket(t *testing.T) {
	socket, err := SocketPath(false)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(socket)
	if !strings.HasPrefix(base, "daemon-") || !strings.HasSuffix(base, "-wpe-headless.sock") {
		t.Fatalf("WPE socket = %s", socket)
	}
}
