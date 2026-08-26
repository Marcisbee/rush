package rush

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLifetimePipeStopsHostWhenParentDisappears(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	stopped := make(chan struct{})
	go stopWhenClosed(reader, func() { close(stopped) })
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("host did not stop after its parent lifetime pipe closed")
	}
}

func TestScopedHostDirectoryOnlyAcceptsManagedCachePaths(t *testing.T) {
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(cache, "rush", "host-123")
	if got, ok := scopedHostDirectory(filepath.Join(directory, "host.sock")); !ok || got != directory {
		t.Fatalf("managed host directory = %q, %v", got, ok)
	}
	for _, socket := range []string{
		filepath.Join(cache, "rush", "host.sock"),
		filepath.Join(cache, "rush", "unmanaged", "host.sock"),
		filepath.Join(t.TempDir(), "host-123", "host.sock"),
		filepath.Join(directory, "different.sock"),
	} {
		if got, ok := scopedHostDirectory(socket); ok {
			t.Fatalf("unmanaged socket %q accepted as %q", socket, got)
		}
	}
}
