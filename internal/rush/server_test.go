package rush

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTimedOutSuiteReportsAStandardFailure(t *testing.T) {
	got := timedOutSuite(BuiltSuite{File: "slow.test.ts"}, 30*time.Second)
	if got.File != "slow.test.ts" || len(got.Tests) != 1 {
		t.Fatalf("timed-out suite = %#v", got)
	}
	result := got.Tests[0]
	if result.Name != "suite execution" || result.Status != "failed" || result.Duration != 30_000 || result.Error != "suite exceeded the configured 30s timeout" {
		t.Fatalf("timed-out result = %#v", result)
	}
	if got.Timing.RunnerMS != 30_000 || got.Timing.TotalMS != 30_000 {
		t.Fatalf("timed-out timing = %#v", got.Timing)
	}
}

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
