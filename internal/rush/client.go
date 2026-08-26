package rush

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Host owns one native browser process for the lifetime of a CLI command.
// Closing the host, or losing the parent-side lifetime pipe, stops the browser
// and prevents an invisible process from surviving its invoking command.
type Host struct {
	socket    string
	directory string
	command   *exec.Cmd
	lifetime  *os.File
	wait      chan error
	closeOnce sync.Once
	closeErr  error
}

func StartHost(headed bool, suiteCount int, sessionDemands ...int) (*Host, error) {
	if headed && !SupportsHeaded() {
		return nil, fmt.Errorf("the %s adapter is headless-only; use the default WebKitGTK build for headed debugging", BackendName())
	}
	if headed && runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return nil, errors.New("headed mode requires DISPLAY or WAYLAND_DISPLAY; start a desktop session or omit --headed")
	}
	if !headed && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("the Rush CLI does not have a native adapter for %s", runtime.GOOS)
	}

	directory, err := createHostDirectory()
	if err != nil {
		return nil, err
	}
	host := &Host{
		socket:    filepath.Join(directory, "host.sock"),
		directory: directory,
		wait:      make(chan error, 1),
	}
	if err := host.start(headed, suiteCount, sessionDemands); err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	return host, nil
}

func createHostDirectory() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(cache, "rush")
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "host-")
}

func (h *Host) start(headed bool, suiteCount int, sessionDemands []int) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer readyReader.Close()
	lifetimeReader, lifetimeWriter, err := os.Pipe()
	if err != nil {
		readyWriter.Close()
		return err
	}

	args := []string{"__host", "--socket", h.socket}
	if headed {
		args = append(args, "--headed")
	}
	if suiteCount > 0 {
		args = append(args, "--suite-count", strconv.Itoa(suiteCount))
	}
	if len(sessionDemands) > 0 {
		values := make([]string, len(sessionDemands))
		for index, demand := range sessionDemands {
			values[index] = strconv.Itoa(demand)
		}
		args = append(args, "--session-demand", strings.Join(values, ","))
	}
	command := exec.Command(executable, args...)
	command.ExtraFiles = []*os.File{readyWriter, lifetimeReader}
	command.Env = append(os.Environ(), "RUSH_READY_FD=3", "RUSH_LIFETIME_FD=4")
	logPath := filepath.Join(h.directory, "host.log")
	log, logErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if logErr != nil {
		readyWriter.Close()
		lifetimeReader.Close()
		lifetimeWriter.Close()
		return logErr
	}
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		log.Close()
		readyWriter.Close()
		lifetimeReader.Close()
		lifetimeWriter.Close()
		return err
	}
	log.Close()
	readyWriter.Close()
	lifetimeReader.Close()

	result := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(readyReader).ReadString('\n')
		result <- strings.TrimSpace(line)
	}()
	select {
	case message := <-result:
		if message != "ready" {
			lifetimeWriter.Close()
			_ = command.Process.Kill()
			_ = command.Wait()
			if strings.HasPrefix(message, "error:") {
				return errors.New(strings.TrimPrefix(message, "error:"))
			}
			return fmt.Errorf("Rush host exited before it became ready; see %s", logPath)
		}
	case <-time.After(20 * time.Second):
		lifetimeWriter.Close()
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("Rush host did not become ready within 20s; see %s", logPath)
	}

	h.command = command
	h.lifetime = lifetimeWriter
	go func() { h.wait <- command.Wait() }()
	return nil
}

func (h *Host) Send(request Request) (Response, error) {
	connection, err := net.DialTimeout("unix", h.socket, 2*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("connect to Rush host: %w", err)
	}
	defer connection.Close()
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != "" {
		return response, errors.New(response.Error)
	}
	return response, nil
}

func (h *Host) Close() error {
	h.closeOnce.Do(func() {
		_, sendErr := h.Send(Request{Action: "shutdown"})
		if sendErr != nil && !errors.Is(sendErr, io.EOF) && !strings.Contains(sendErr.Error(), "connect") {
			h.closeErr = sendErr
		}
		if h.lifetime != nil {
			_ = h.lifetime.Close()
		}
		select {
		case waitErr := <-h.wait:
			if waitErr != nil && h.closeErr == nil {
				h.closeErr = waitErr
			}
		case <-time.After(2 * time.Second):
			if h.command != nil && h.command.Process != nil {
				_ = h.command.Process.Kill()
			}
			<-h.wait
			if h.closeErr == nil {
				h.closeErr = errors.New("Rush host did not stop within 2s and was killed")
			}
		}
		if removeErr := os.RemoveAll(h.directory); removeErr != nil && h.closeErr == nil {
			h.closeErr = removeErr
		}
	})
	return h.closeErr
}
