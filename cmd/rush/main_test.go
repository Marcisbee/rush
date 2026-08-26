package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMedian(t *testing.T) {
	if got := median([]float64{9, 1, 4, 2, 8}); got != 4 {
		t.Fatalf("median = %v", got)
	}
}

func TestMergeWatchFilesMakesPathsAbsoluteAndUnique(t *testing.T) {
	cwd := t.TempDir()
	got := mergeWatchFiles(cwd, []string{"test/example.test.ts"}, []string{
		filepath.Join(cwd, "src/example.ts"),
		"test/example.test.ts",
	})
	want := []string{
		filepath.Join(cwd, "src/example.ts"),
		filepath.Join(cwd, "test/example.test.ts"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("watch files = %#v; want %#v", got, want)
	}
}

func TestWaitForFileChangeDetectsAnEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "example.ts")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	changed := make(chan string, 1)
	errs := make(chan error, 1)
	go func() {
		path, err := waitForFileChange(ctx, []string{path})
		changed <- path
		errs <- err
	}()
	time.Sleep(200 * time.Millisecond)
	if err := os.WriteFile(path, []byte("after edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := <-changed; got != path {
		t.Fatalf("changed file = %q; want %q", got, path)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func TestWaitForFileChangeStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForFileChange(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v", err)
	}
}
