package rush

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	socket  string
	browser *Browser
	builder *Builder
	started time.Time
	cold    atomic.Bool
	runMu   sync.Mutex
	nextID  atomic.Uint64
}

func RunDaemon(socket string, headed bool, ready *os.File) error {
	started := time.Now()
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

	browser, err := NewBrowser(headed)
	if err != nil {
		writeReady(ready, err)
		return err
	}
	defer browser.Close()
	server := &Server{socket: socket, browser: browser, builder: NewBuilder(), started: started}
	server.cold.Store(true)
	defer server.builder.Close()

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
			browser.Stop()
		}
	}()
	browser.RunLoop()
	return nil
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
	for display := 90; display < 190; display++ {
		socket := filepath.Join("/tmp/.X11-unix", "X"+strconv.Itoa(display))
		if _, err := os.Stat(socket); err == nil {
			continue
		}
		authPath, err := createXAuthority(xauth, display)
		if err != nil {
			return nil, err
		}
		command := exec.Command(xvfb, ":"+strconv.Itoa(display), "-nolisten", "tcp", "-auth", authPath, "-screen", "0", "1280x800x24")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			_ = os.Remove(authPath)
			return nil, fmt.Errorf("start Xvfb: %w", err)
		}
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case processErr := <-exited:
				_ = os.Remove(authPath)
				return nil, fmt.Errorf("Xvfb exited before allocating display %d: %w", display, processErr)
			default:
			}
			connection, dialErr := net.DialTimeout("unix", socket, 50*time.Millisecond)
			if dialErr == nil {
				connection.Close()
				if err := os.Setenv("DISPLAY", ":"+strconv.Itoa(display)); err != nil {
					_ = command.Process.Kill()
					<-exited
					_ = os.Remove(authPath)
					return nil, err
				}
				if err := os.Setenv("XAUTHORITY", authPath); err != nil {
					_ = command.Process.Kill()
					<-exited
					_ = os.Remove(authPath)
					return nil, err
				}
				return func() {
					_ = command.Process.Kill()
					<-exited
					_ = os.Remove(authPath)
				}, nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		_ = command.Process.Kill()
		<-exited
		_ = os.Remove(authPath)
	}
	return nil, errors.New("Xvfb could not allocate a Unix display")
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
	var authPath string
	fail := func(err error) (func(), error) {
		_ = os.Remove(authPath)
		return nil, err
	}

	for display := 90; display < 190; display++ {
		port := 6000 + display
		probe, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if listenErr != nil {
			continue
		}
		probe.Close()
		authPath, err = createXAuthority(xauth, display)
		if err != nil {
			return fail(err)
		}
		command := exec.Command(xvfb, ":"+strconv.Itoa(display), "-nolisten", "unix", "-listen", "tcp", "-auth", authPath, "-screen", "0", "1280x800x24")
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Start(); err != nil {
			return fail(fmt.Errorf("start Xvfb: %w", err))
		}
		exited := make(chan error, 1)
		go func() { exited <- command.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case processErr := <-exited:
				return fail(fmt.Errorf("Xvfb exited before allocating display %d: %w", display, processErr))
			default:
			}
			connection, dialErr := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 50*time.Millisecond)
			if dialErr == nil {
				connection.Close()
				if err := os.Setenv("DISPLAY", "localhost:"+strconv.Itoa(display)); err != nil {
					_ = command.Process.Kill()
					<-exited
					return fail(err)
				}
				if err := os.Setenv("XAUTHORITY", authPath); err != nil {
					_ = command.Process.Kill()
					<-exited
					return fail(err)
				}
				return func() {
					_ = command.Process.Kill()
					<-exited
					_ = os.Remove(authPath)
				}, nil
			}
			time.Sleep(25 * time.Millisecond)
		}
		_ = command.Process.Kill()
		<-exited
	}
	return fail(errors.New("Xvfb could not allocate a loopback display"))
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
			s.browser.Stop()
		}()
		return
	}
	if request.Action != "run" {
		_ = encoder.Encode(Response{Error: "unknown daemon action: " + request.Action})
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
	timeout := 30 * time.Second
	if request.Timeout > 0 {
		timeout = time.Duration(request.Timeout) * time.Millisecond
	}
	for _, file := range request.Files {
		source, buildMS, err := s.builder.Build(request.CWD, file)
		if err != nil {
			response.Error = err.Error()
			response.WallMS = milliseconds(time.Since(started))
			return response
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		id := fmt.Sprintf("run-%d", s.nextID.Add(1))
		suite, err := s.browser.Run(ctx, id, file, source)
		cancel()
		if err != nil {
			response.Error = fmt.Sprintf("run %s: %v", file, err)
			response.WallMS = milliseconds(time.Since(started))
			return response
		}
		suite.Timing.BuildMS = buildMS
		response.Suites = append(response.Suites, suite)
	}
	response.WallMS = milliseconds(time.Since(started))
	return response
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
