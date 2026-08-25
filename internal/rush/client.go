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
	"strings"
	"syscall"
	"time"
)

func SocketPath(headed bool) (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	mode := "headless"
	if headed {
		mode = "headed"
	}
	return filepath.Join(cache, "rush", "daemon-"+mode+".sock"), nil
}

func Send(request Request, headed bool) (Response, error) {
	socket, err := SocketPath(headed)
	if err != nil {
		return Response{}, err
	}
	connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		if request.Action != "run" {
			return Response{}, err
		}
		if err := spawnDaemon(socket, headed); err != nil {
			return Response{}, err
		}
		connection, err = net.DialTimeout("unix", socket, 2*time.Second)
		if err != nil {
			return Response{}, fmt.Errorf("connect to Rush daemon after startup: %w", err)
		}
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

func Stop(headed bool) error {
	socket, pathErr := SocketPath(headed)
	if pathErr != nil {
		return pathErr
	}
	_, err := Send(Request{Action: "shutdown"}, headed)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrNotExist) && !strings.Contains(err.Error(), "connect:") {
		return err
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(socket); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("Rush daemon did not stop within 2s")
}

func spawnDaemon(socket string, headed bool) error {
	if err := os.MkdirAll(filepath.Dir(socket), 0700); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	args := []string{"__daemon", "--socket", socket}
	var command *exec.Cmd
	if headed {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			writer.Close()
			return errors.New("headed mode requires DISPLAY or WAYLAND_DISPLAY; start a desktop session or omit --headed")
		}
		args = append(args, "--headed")
		command = exec.Command(executable, args...)
	} else {
		if runtime.GOOS != "linux" {
			writer.Close()
			return fmt.Errorf("the initial Rush adapter supports Linux only (running %s)", runtime.GOOS)
		}
		command = exec.Command(executable, args...)
	}
	command.ExtraFiles = []*os.File{writer}
	command.Env = append(os.Environ(), "RUSH_READY_FD=3")
	logPath := socket + ".log"
	log, logErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if logErr != nil {
		writer.Close()
		return logErr
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		writer.Close()
		return err
	}
	writer.Close()

	result := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(reader).ReadString('\n')
		result <- strings.TrimSpace(line)
	}()
	select {
	case message := <-result:
		if message == "ready" {
			return nil
		}
		if strings.HasPrefix(message, "error:") {
			return errors.New(strings.TrimPrefix(message, "error:"))
		}
		return fmt.Errorf("Rush daemon exited before it became ready; see %s", logPath)
	case <-time.After(20 * time.Second):
		return fmt.Errorf("Rush daemon did not become ready within 20s; see %s", logPath)
	}
}
