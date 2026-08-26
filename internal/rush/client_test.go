package rush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathScopesDaemonToExecutableAndMode(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	headless, err := SocketPath(false)
	if err != nil {
		t.Fatal(err)
	}
	headed, err := SocketPath(true)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(headless) != filepath.Join(cache, "rush") {
		t.Fatalf("socket directory = %q", filepath.Dir(headless))
	}
	if !strings.HasSuffix(headless, "-"+backendSocketMode(false)+".sock") || !strings.HasSuffix(headed, "-"+backendSocketMode(true)+".sock") {
		t.Fatalf("unexpected socket modes: %q %q", headless, headed)
	}
	if headless == headed || !strings.HasPrefix(filepath.Base(headless), "daemon-") {
		t.Fatalf("socket identity is not scoped: %q %q", headless, headed)
	}
}
