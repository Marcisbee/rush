package rush

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Server struct {
	socket   string
	browser  *BrowserPool
	builder  *Builder
	started  time.Time
	cold     atomic.Bool
	runMu    sync.Mutex
	stopOnce sync.Once
	nextID   atomic.Uint64
}

func RunHost(socket string, headed bool, suiteCount int, sessionDemands []int, ready, lifetime *os.File) error {
	started := time.Now()
	if directory, ok := scopedHostDirectory(socket); ok {
		defer os.RemoveAll(directory)
	}
	if lifetime != nil {
		defer lifetime.Close()
	}
	stopDisplay, err := prepareBrowser(headed)
	if err != nil {
		writeReady(ready, err)
		return err
	}
	defer stopDisplay()
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		writeReady(ready, err)
		return err
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		writeReady(ready, err)
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		writeReady(ready, err)
		return err
	}
	defer listener.Close()
	defer os.Remove(socket)
	if err := os.Chmod(socket, 0600); err != nil {
		writeReady(ready, err)
		return err
	}

	poolSize, err := configuredBrowserPoolSize(headed, os.Getenv("RUSH_WEBVIEW_POOL_SIZE"), suiteCount)
	if err != nil {
		writeReady(ready, err)
		return err
	}
	browser, err := NewBrowserPool(headed, poolSize, sessionDemands...)
	if err != nil {
		writeReady(ready, err)
		return err
	}
	defer browser.Close()
	server := &Server{socket: socket, browser: browser, builder: NewBuilder(), started: started}
	server.cold.Store(true)
	defer server.builder.Close()
	if lifetime != nil {
		go stopWhenClosed(lifetime, server.stop)
	}

	go func() {
		select {
		case <-browser.Ready():
			writeReady(ready, nil)
			for {
				connection, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				go server.handle(connection)
			}
		case <-time.After(15 * time.Second):
			writeReady(ready, fmt.Errorf("%s page did not become ready within 15s", BackendName()))
			server.stop()
		}
	}()
	browser.RunLoop()
	return nil
}

func (s *Server) stop() {
	s.stopOnce.Do(s.browser.Stop)
}

func scopedHostDirectory(socket string) (string, bool) {
	if filepath.Base(socket) != "host.sock" {
		return "", false
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", false
	}
	root, err := filepath.Abs(filepath.Join(cache, "rush"))
	if err != nil {
		return "", false
	}
	directory, err := filepath.Abs(filepath.Dir(socket))
	if err != nil || filepath.Dir(directory) != root || !strings.HasPrefix(filepath.Base(directory), "host-") {
		return "", false
	}
	return directory, true
}

func stopWhenClosed(lifetime io.Reader, stop func()) {
	_, _ = io.Copy(io.Discard, lifetime)
	stop()
}

func startVirtualDisplay() (func(), error) {
	xvfb, err := exec.LookPath("Xvfb")
	if err != nil {
		return nil, errors.New("headless mode requires Xvfb; install the xvfb system package")
	}
	if info, statErr := os.Stat("/tmp/.X11-unix"); statErr == nil && info.Mode()&os.ModeSticky == 0 {
		return startTCPVirtualDisplay(xvfb)
	}
	return startUnixVirtualDisplay(xvfb)
}

func startUnixVirtualDisplay(xvfb string) (func(), error) {
	xauth, err := exec.LookPath("xauth")
	if err != nil {
		return nil, errors.New("headless mode requires xauth to protect the Xvfb display")
	}
	lockDirectory, err := virtualDisplayLockDirectory()
	if err != nil {
		return nil, fmt.Errorf("prepare Xvfb display locks: %w", err)
	}
	var lastErr error
	for display := 90; display < 190; display++ {
		releaseDisplay, claimed, err := claimVirtualDisplay(lockDirectory, display)
		if err != nil {
			return nil, fmt.Errorf("claim Xvfb display %d: %w", display, err)
		}
		if !claimed {
			continue
		}
		socket := filepath.Join("/tmp/.X11-unix", "X"+strconv.Itoa(display))
		if _, err := os.Stat(socket); err == nil {
			releaseDisplay()
			continue
		}
		authPath, err := createXAuthority(xauth, display)
		if err != nil {
			releaseDisplay()
			return nil, err
		}
		command := exec.Command(xvfb, ":"+strconv.Itoa(display), "-nolisten", "tcp", "-auth", authPath, "-screen", "0", "1280x800x24")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			_ = os.Remove(authPath)
			releaseDisplay()
			return nil, fmt.Errorf("start Xvfb: %w", err)
		}
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		stop := func() {
			_ = command.Process.Kill()
			<-exited
			_ = os.Remove(authPath)
			releaseDisplay()
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case processErr := <-exited:
				_ = os.Remove(authPath)
				releaseDisplay()
				lastErr = fmt.Errorf("Xvfb exited before allocating display %d: %w", display, processErr)
				goto nextUnixDisplay
			default:
			}
			connection, dialErr := net.DialTimeout("unix", socket, 50*time.Millisecond)
			if dialErr == nil {
				connection.Close()
				if err := os.Setenv("DISPLAY", ":"+strconv.Itoa(display)); err != nil {
					stop()
					return nil, err
				}
				if err := os.Setenv("XAUTHORITY", authPath); err != nil {
					stop()
					return nil, err
				}
				return stop, nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		stop()
		lastErr = fmt.Errorf("Xvfb did not allocate display %d within 5s", display)
	nextUnixDisplay:
	}
	if lastErr != nil {
		return nil, fmt.Errorf("Xvfb could not allocate a Unix display: %w", lastErr)
	}
	return nil, errors.New("Xvfb could not allocate a Unix display")
}

func virtualDisplayLockDirectory() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	directory := filepath.Join(cache, "rush", "display-locks")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	return directory, nil
}

func createXAuthority(xauth string, display int) (string, error) {
	auth, err := os.CreateTemp("", "rush-xauthority-")
	if err != nil {
		return "", err
	}
	authPath := auth.Name()
	auth.Close()
	_ = os.Remove(authPath)
	cookieBytes := make([]byte, 16)
	if _, err := rand.Read(cookieBytes); err != nil {
		return "", err
	}
	cookie := hex.EncodeToString(cookieBytes)
	if output, err := exec.Command(xauth, "-f", authPath, "add", ":"+strconv.Itoa(display), ".", cookie).CombinedOutput(); err != nil {
		_ = os.Remove(authPath)
		return "", fmt.Errorf("create Xvfb authority: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return authPath, nil
}

// Some container and WSL mounts expose /tmp/.X11-unix without the sticky bit
// and make it read-only. Xorg refuses Unix sockets there, so use an authenticated
// loopback TCP display instead of weakening access control with -ac.
func startTCPVirtualDisplay(xvfb string) (func(), error) {
	xauth, err := exec.LookPath("xauth")
	if err != nil {
		return nil, errors.New("/tmp/.X11-unix is not usable and the authenticated TCP fallback requires xauth")
	}
	lockDirectory, err := virtualDisplayLockDirectory()
	if err != nil {
		return nil, fmt.Errorf("prepare Xvfb display locks: %w", err)
	}
	var lastErr error
	for display := 90; display < 190; display++ {
		releaseDisplay, claimed, err := claimVirtualDisplay(lockDirectory, display)
		if err != nil {
			return nil, fmt.Errorf("claim Xvfb display %d: %w", display, err)
		}
		if !claimed {
			continue
		}
		port := 6000 + display
		probe, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if listenErr != nil {
			releaseDisplay()
			continue
		}
		probe.Close()
		authPath, err := createXAuthority(xauth, display)
		if err != nil {
			releaseDisplay()
			return nil, err
		}
		command := exec.Command(xvfb, ":"+strconv.Itoa(display), "-nolisten", "unix", "-listen", "tcp", "-auth", authPath, "-screen", "0", "1280x800x24")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			_ = os.Remove(authPath)
			releaseDisplay()
			return nil, fmt.Errorf("start Xvfb: %w", err)
		}
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		stop := func() {
			_ = command.Process.Kill()
			<-exited
			_ = os.Remove(authPath)
			releaseDisplay()
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case processErr := <-exited:
				_ = os.Remove(authPath)
				releaseDisplay()
				lastErr = fmt.Errorf("Xvfb exited before allocating display %d: %w", display, processErr)
				goto nextTCPDisplay
			default:
			}
			connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 50*time.Millisecond)
			if dialErr == nil {
				connection.Close()
				if err := os.Setenv("DISPLAY", "localhost:"+strconv.Itoa(display)); err != nil {
					stop()
					return nil, err
				}
				if err := os.Setenv("XAUTHORITY", authPath); err != nil {
					stop()
					return nil, err
				}
				return stop, nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		stop()
		lastErr = fmt.Errorf("Xvfb did not allocate display %d within 5s", display)
	nextTCPDisplay:
	}
	if lastErr != nil {
		return nil, fmt.Errorf("Xvfb could not allocate a loopback display: %w", lastErr)
	}
	return nil, errors.New("Xvfb could not allocate a loopback display")
}

func (s *Server) handle(connection net.Conn) {
	defer connection.Close()
	decoder := json.NewDecoder(bufio.NewReader(connection))
	encoder := json.NewEncoder(connection)
	var request Request
	if err := decoder.Decode(&request); err != nil {
		_ = encoder.Encode(Response{Error: fmt.Sprintf("decode request: %v", err)})
		return
	}
	if request.Action == "ping" {
		_ = encoder.Encode(Response{})
		return
	}
	if request.Action == "shutdown" {
		_ = encoder.Encode(Response{})
		go func() {
			time.Sleep(25 * time.Millisecond)
			s.stop()
		}()
		return
	}
	if request.Action != "run" {
		_ = encoder.Encode(Response{Error: "unknown host action: " + request.Action})
		return
	}

	s.runMu.Lock()
	defer s.runMu.Unlock()
	response := s.run(request)
	_ = encoder.Encode(response)
}

func (s *Server) run(request Request) Response {
	started := time.Now()
	response := Response{
		Cold:      s.cold.Swap(false),
		StartupMS: milliseconds(time.Since(s.started)),
	}
	response.Profile.BrowserRealms = s.browser.Size()
	timeout := 30 * time.Second
	if request.Timeout > 0 {
		timeout = time.Duration(request.Timeout) * time.Millisecond
	}
	bundles, buildMS := request.Bundles, request.BuildMS
	response.WatchFiles = append([]string(nil), request.WatchFiles...)
	if len(bundles) == 0 {
		var err error
		bundles, buildMS, err = s.builder.BuildBatch(request.CWD, request.Files)
		response.WatchFiles = s.builder.WatchFiles()
		if err != nil {
			response.Error = err.Error()
			response.WallMS = milliseconds(time.Since(started))
			return response
		}
	}
	response.Profile.BundleMS = buildMS
	browserStarted := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout*time.Duration(len(request.Files)))
	id := fmt.Sprintf("run-%d", s.nextID.Add(1))
	batch, err := s.browser.RunBatch(ctx, id, bundles)
	cancel()
	browserRoundtripMS := milliseconds(time.Since(browserStarted))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && len(bundles) == 1 {
			response.Suites = []SuiteResult{timedOutSuite(bundles[0], timeout)}
			response.Profile.BridgeMS = browserRoundtripMS
			response.WallMS = milliseconds(time.Since(started))
			response.Profile.NativeHostMS = max(0, response.WallMS-buildMS-browserRoundtripMS)
			return response
		}
		response.Error = fmt.Sprintf("run suites: %v", err)
		response.WallMS = milliseconds(time.Since(started))
		return response
	}
	if len(batch.Suites) != len(bundles) {
		response.Error = fmt.Sprintf("browser returned %d suites for %d bundles", len(batch.Suites), len(bundles))
		response.WallMS = milliseconds(time.Since(started))
		return response
	}
	perSuiteBuildMS := buildMS / float64(len(batch.Suites))
	for index := range batch.Suites {
		batch.Suites[index].Timing.BuildMS = perSuiteBuildMS
		response.Profile.TestExecutionMS += batch.Suites[index].Timing.TotalMS
		response.Profile.ResetMS += batch.Suites[index].Timing.ResetMS
	}
	response.Profile.BrowserExecutionMS = batch.BrowserMS
	response.Profile.ReportingMS = batch.ReportingMS
	response.Profile.BridgeMS = max(0, browserRoundtripMS-batch.BrowserMS-batch.ReportingMS)
	response.Suites = batch.Suites
	response.WallMS = milliseconds(time.Since(started))
	response.Profile.NativeHostMS = max(0, response.WallMS-buildMS-browserRoundtripMS)
	return response
}

func timedOutSuite(bundle BuiltSuite, timeout time.Duration) SuiteResult {
	duration := milliseconds(timeout)
	return SuiteResult{
		File: bundle.File,
		Tests: []TestResult{{
			Name:     "suite execution",
			Status:   "failed",
			Duration: duration,
			Error:    fmt.Sprintf("suite exceeded the configured %s timeout", timeout),
		}},
		Timing: Timing{RunnerMS: duration, TotalMS: duration},
	}
}

func writeReady(file *os.File, err error) {
	if file == nil {
		return
	}
	defer file.Close()
	if err != nil {
		_, _ = fmt.Fprintf(file, "error:%v\n", err)
		return
	}
	_, _ = fmt.Fprintln(file, "ready")
}
