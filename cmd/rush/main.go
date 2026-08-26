package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Marcisbee/rush/internal/rush"
)

var errTestsFailed = errors.New("tests failed")

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if err != errTestsFailed {
			fmt.Fprintln(os.Stderr, "rush:", err)
		}
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: rush test [--watch] [--headed] [--verbose] [--json] FILE... | rush bench | rush doctor")
	}
	switch args[0] {
	case "test":
		return runTests(args[1:], stdout, stderr)
	case "bench":
		return runBenchmarks(args[1:], stdout)
	case "doctor":
		return doctor(stdout)
	case "__host":
		return nativeHost(args[1:])
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

func runTests(args []string, stdout, stderr io.Writer) (runErr error) {
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	set.SetOutput(stderr)
	headedHelp := "show " + rush.BackendName() + " with inspector support"
	if !rush.SupportsHeaded() {
		headedHelp = "unsupported by the WPE headless adapter"
	}
	headed := set.Bool("headed", false, headedHelp)
	watch := set.Bool("watch", false, "rerun tests when their source dependencies change")
	jsonOutput := set.Bool("json", false, "write a machine-readable response")
	verbose := set.Bool("verbose", false, "show detailed build and browser timing phases")
	timeout := set.Duration("timeout", 30*time.Second, "timeout for each suite")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *watch && *jsonOutput {
		return errors.New("--json cannot be combined with --watch")
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
	sessionDemands, err := detectSessionDemands(files)
	if err != nil {
		return err
	}
	type hostResult struct {
		host *rush.Host
		err  error
	}
	hostReady := make(chan hostResult, 1)
	go func() {
		host, hostErr := rush.StartHost(*headed, len(files), sessionDemands...)
		hostReady <- hostResult{host: host, err: hostErr}
	}()
	builder := rush.NewBuilder()
	defer builder.Close()
	bundles, buildMS, buildErr := builder.BuildBatch(cwd, files)
	startedHost := <-hostReady
	if startedHost.err != nil {
		if startedHost.host != nil {
			_ = startedHost.host.Close()
		}
		return errors.Join(startedHost.err, buildErr)
	}
	host := startedHost.host
	defer func() {
		if closeErr := host.Close(); closeErr != nil {
			runErr = errors.Join(runErr, closeErr)
		}
	}()
	watchContext := context.Background()
	if *watch {
		ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt)
		watchContext = ctx
		defer stopSignals()
		go func() {
			<-ctx.Done()
			_ = host.Close()
		}()
	}
	request := rush.Request{
		Action: "run", CWD: cwd, Files: files, Bundles: bundles, BuildMS: buildMS,
		WatchFiles: builder.WatchFiles(), Timeout: timeout.Milliseconds(),
	}
	var response rush.Response
	if buildErr != nil {
		err = buildErr
		response.WatchFiles = builder.WatchFiles()
	} else {
		response, err = host.Send(request)
	}
	if *watch && watchContext.Err() != nil {
		return nil
	}
	if err != nil && !*watch {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(response)
	}
	console := consoleOptionsFor(stdout, *verbose)
	if err != nil {
		fmt.Fprintln(stderr, "rush:", err)
	} else if printErr := printResponse(stdout, response, console); printErr != nil && !*watch {
		return printErr
	}
	if !*watch {
		return nil
	}

	watched := mergeWatchFiles(cwd, files, response.WatchFiles)
	fmt.Fprintln(stdout, "Watching for changes; press Ctrl+C to stop.")
	for {
		changed, waitErr := waitForFileChange(watchContext, watched)
		if errors.Is(waitErr, context.Canceled) {
			return nil
		}
		if waitErr != nil {
			return waitErr
		}
		printWatchChange(stdout, displayPath(cwd, changed), console)
		bundles, buildMS, err = builder.BuildBatch(cwd, files)
		if err != nil {
			fmt.Fprintln(stderr, "rush:", err)
			watched = mergeWatchFiles(cwd, files, builder.WatchFiles())
			continue
		}
		request.Bundles = bundles
		request.BuildMS = buildMS
		request.WatchFiles = builder.WatchFiles()
		response, err = host.Send(request)
		if watchContext.Err() != nil {
			return nil
		}
		if len(response.WatchFiles) > 0 {
			watched = mergeWatchFiles(cwd, files, response.WatchFiles)
		}
		if err != nil {
			fmt.Fprintln(stderr, "rush:", err)
			continue
		}
		_ = printResponse(stdout, response, console)
	}
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

func nativeHost(args []string) error {
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)
	set := flag.NewFlagSet("__host", flag.ContinueOnError)
	socket := set.String("socket", "", "host socket")
	headed := set.Bool("headed", false, "show the browser")
	suiteCount := set.Int("suite-count", 0, "number of suites in the invoking command")
	sessionDemand := set.String("session-demand", "", "comma-separated session clients required by each suite")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *socket == "" {
		return errors.New("host socket is required")
	}
	var ready *os.File
	if raw := os.Getenv("RUSH_READY_FD"); raw != "" {
		fd, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		ready = os.NewFile(uintptr(fd), "rush-ready")
	}
	var lifetime *os.File
	if raw := os.Getenv("RUSH_LIFETIME_FD"); raw != "" {
		fd, err := strconv.Atoi(raw)
		if err != nil {
			return err
		}
		lifetime = os.NewFile(uintptr(fd), "rush-lifetime")
	}
	demands, err := parseSessionDemands(*sessionDemand)
	if err != nil {
		return err
	}
	return rush.RunHost(*socket, *headed, *suiteCount, demands, ready, lifetime)
}

func parseSessionDemands(value string) ([]int, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	demands := make([]int, len(parts))
	for index, part := range parts {
		demand, err := strconv.Atoi(part)
		if err != nil || demand < 0 || demand > 4 {
			return nil, fmt.Errorf("invalid session demand %q", part)
		}
		demands[index] = demand
	}
	return demands, nil
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
