package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Marcisbee/rush/internal/rush"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "rush:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: rush test [--headed] [--json] FILE... | rush bench | rush doctor | rush stop")
	}
	switch args[0] {
	case "test":
		return runTests(args[1:], stdout, stderr)
	case "bench":
		return runBenchmarks(args[1:], stdout)
	case "doctor":
		return doctor(stdout)
	case "stop":
		return stop(args[1:])
	case "__daemon":
		return daemon(args[1:])
	case "__session-worker":
		return sessionWorker(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func sessionWorker(args []string) error {
	set := flag.NewFlagSet("__session-worker", flag.ContinueOnError)
	headed := set.Bool("headed", false, "show the session browser")
	if err := set.Parse(args); err != nil {
		return err
	}
	return rush.SessionWorkerMain(*headed)
}

func runTests(args []string, stdout, stderr io.Writer) error {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(stderr)
	headedHelp := "show " + rush.BackendName() + " with inspector support"
	if !rush.SupportsHeaded() {
		headedHelp = "unsupported by the WPE headless adapter"
	}
	headed := set.Bool("headed", false, headedHelp)
	jsonOutput := set.Bool("json", false, "write a machine-readable response")
	timeout := set.Duration("timeout", 30*time.Second, "timeout for each suite")
	if err := set.Parse(args); err != nil {
		return err
	}
	files, err := expandFiles(set.Args())
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return errors.New("no test files supplied")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	response, err := rush.Send(rush.Request{Action: "run", CWD: cwd, Files: files, Timeout: timeout.Milliseconds()}, *headed)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	}
	return printResponse(stdout, response)
}

func expandFiles(arguments []string) ([]string, error) {
	seen := make(map[string]bool)
	var files []string
	for _, argument := range arguments {
		matches, err := filepath.Glob(argument)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			matches = []string{argument}
		}
		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil {
				return nil, err
			}
			if info.IsDir() {
				return nil, fmt.Errorf("%s is a directory; pass test files or a shell glob", match)
			}
			clean := filepath.Clean(match)
			if !seen[clean] {
				seen[clean] = true
				files = append(files, clean)
			}
		}
	}
	return files, nil
}

func printResponse(output io.Writer, response rush.Response) error {
	passed, failed, skipped := 0, 0, 0
	for _, suite := range response.Suites {
		for _, test := range suite.Tests {
			switch test.Status {
			case "passed":
				passed++
			case "failed":
				failed++
				fmt.Fprintf(output, "FAIL %s — %s\n%s\n", suite.File, test.Name, test.Error)
			default:
				skipped++
			}
		}
		t := suite.Timing
		fmt.Fprintf(output, "%s: build %.2fms | runner %.2fms | application %.2fms | network %.2fms | intentional wait %.2fms | page total %.2fms\n",
			suite.File, t.BuildMS, t.RunnerMS, t.ApplicationMS, t.NetworkMS, t.WaitMS, t.TotalMS)
	}
	fmt.Fprintf(output, "%d passed, %d failed, %d skipped; request %.2fms", passed, failed, skipped, response.WallMS)
	if response.Cold {
		fmt.Fprintf(output, "; cold browser startup %.2fms", response.StartupMS)
	}
	fmt.Fprintln(output)
	if failed > 0 {
		return fmt.Errorf("%d test(s) failed", failed)
	}
	return nil
}

func daemon(args []string) error {
	set := flag.NewFlagSet("__daemon", flag.ContinueOnError)
	socket := set.String("socket", "", "daemon socket")
	headed := set.Bool("headed", false, "show the browser")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *socket == "" {
		return errors.New("daemon socket is required")
	}
	var ready *os.File
	if raw := os.Getenv("RUSH_READY_FD"); raw != "" {
		fd, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		ready = os.NewFile(uintptr(fd), "rush-ready")
	}
	return rush.RunDaemon(*socket, *headed, ready)
}

func stop(args []string) error {
	set := flag.NewFlagSet("stop", flag.ContinueOnError)
	headed := set.Bool("headed", false, "stop the headed daemon instead")
	if err := set.Parse(args); err != nil {
		return err
	}
	return rush.Stop(*headed)
}

func doctor(output io.Writer) error {
	return rush.Doctor(output)
}

func failedTests(response rush.Response) int {
	count := 0
	for _, suite := range response.Suites {
		for _, test := range suite.Tests {
			if test.Status == "failed" {
				count++
			}
		}
	}
	return count
}

func median(values []float64) float64 {
	copyOfValues := append([]float64(nil), values...)
	for i := 0; i < len(copyOfValues); i++ {
		for j := i + 1; j < len(copyOfValues); j++ {
			if copyOfValues[j] < copyOfValues[i] {
				copyOfValues[i], copyOfValues[j] = copyOfValues[j], copyOfValues[i]
			}
		}
	}
	return copyOfValues[len(copyOfValues)/2]
}

func repoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for candidate := cwd; ; candidate = filepath.Dir(candidate) {
		if data, readErr := os.ReadFile(filepath.Join(candidate, "go.mod")); readErr == nil && strings.Contains(string(data), "module github.com/Marcisbee/rush") {
			return candidate, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", errors.New("run benchmarks from the Rush repository")
		}
	}
}
