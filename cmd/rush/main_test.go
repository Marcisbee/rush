package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Marcisbee/rush/internal/rush"
)

func TestMedian(t *testing.T) {
	if got := median([]float64{9, 1, 4, 2, 8}); got != 4 {
		t.Fatalf("median = %v", got)
	}
}

func TestConsoleReporterUsesCompactFileAndTestHierarchy(t *testing.T) {
	response := rush.Response{
		Cold: true, StartupMS: 75, WallMS: 50,
		Suites: []rush.SuiteResult{
			{
				File: "first.test.ts",
				Tests: []rush.TestResult{
					{Name: "adds values", Status: "passed", Duration: 0.5},
					{Name: "reports failures", Status: "failed", Duration: 2, Error: "expected: yes\nreceived: no"},
				},
				Timing: rush.Timing{BuildMS: 3, RunnerMS: 4, ApplicationMS: 2, TotalMS: 6},
			},
			{File: "second.test.ts", Tests: []rush.TestResult{
				{Name: "skipped", Status: "skipped"},
				{Name: "pending", Status: "todo"},
			}},
		},
	}
	var output bytes.Buffer
	err := printResponse(&output, response, consoleOptions{verbose: true})
	if err != errTestsFailed {
		t.Fatalf("reporter error = %v; want reported failure sentinel", err)
	}
	for _, expected := range []string{
		"first.test.ts:\n(pass) adds values [0.50ms]",
		"(fail) reports failures [2.00ms]\n  expected: yes\n  received: no",
		"build 3.00ms | runner 4.00ms | application 2.00ms",
		"second.test.ts:\n(skip) skipped\n(todo) pending",
		" 1 pass\n 1 fail\n 1 skip\n 1 todo",
		"Ran 4 tests across 2 files. [125.00ms]",
		"startup 75.00ms | request 50.00ms",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output %q does not contain %q", output.String(), expected)
		}
	}
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("non-terminal output contains ANSI escapes: %q", output.String())
	}
}

func TestFailureFormatterCompactsTestingLibraryDiagnostics(t *testing.T) {
	raw := "TestingLibraryElementError: Unable to find button\n\n" +
		"Here are the accessible roles:\n\n" +
		"  main:\n\n" +
		"  Name \"\":\n" +
		"  <main />\n\n" +
		"  --------------------------------------------------\n" +
		"  button:\n\n" +
		"  Name \"Wrong\":\n" +
		"  <button />\n\n" +
		"Ignored nodes: comments, script, style\n" +
		"<body>\n  <main />\n</body>\n" +
		"getElementError@\n@\nrunTest@\n"
	got := strings.Join(formatFailure(raw), "\n")
	want := "TestingLibraryElementError: Unable to find button\n\n" +
		"Accessible roles:\n" +
		"  main:\n" +
		"    Name \"\":\n" +
		"    <main />\n" +
		"  button:\n" +
		"    Name \"Wrong\":\n" +
		"    <button />\n\n" +
		"Ignored nodes: comments, script, style\n" +
		"<body>\n  <main />\n</body>"
	if got != want {
		t.Fatalf("formatted failure:\n%s\n\nwant:\n%s", got, want)
	}
}

func TestConsoleReporterUsesColorAndSymbolsForInteractiveOutput(t *testing.T) {
	response := rush.Response{Suites: []rush.SuiteResult{{
		File:  "color.test.ts",
		Tests: []rush.TestResult{{Name: "works", Status: "passed", Duration: 1}},
	}}}
	var output bytes.Buffer
	if err := printResponse(&output, response, consoleOptions{color: true}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"\x1b[1mcolor.test.ts:\x1b[0m", "\x1b[32m✓\x1b[0m works", "\x1b[2m[1.00ms]\x1b[0m"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("output %q does not contain %q", output.String(), expected)
		}
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
