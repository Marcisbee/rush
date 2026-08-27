//go:build linux

package rush

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestVirtualDisplayClaimReusesStaleLockAndCleansUp(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "X90.lock")
	if err := os.WriteFile(path, []byte("999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	release, claimed, err := claimVirtualDisplay(directory, 90)
	if err != nil {
		t.Fatal(err)
	}
	if !claimed {
		t.Fatal("stale display lock was not reclaimed")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(contents), strconv.Itoa(os.Getpid())+"\n"; got != want {
		t.Fatalf("lock owner = %q, want %q", got, want)
	}

	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("display lock remains after release: %v", err)
	}
}

func TestConcurrentRushProcessesClaimDistinctVirtualDisplays(t *testing.T) {
	const processCount = 4
	directory := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type childProcess struct {
		command *exec.Cmd
		input   io.WriteCloser
		output  *bufio.Reader
		stderr  bytes.Buffer
	}
	children := make([]*childProcess, 0, processCount)
	for index := 0; index < processCount; index++ {
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestVirtualDisplayClaimHelper$")
		command.Env = append(os.Environ(),
			"RUSH_TEST_DISPLAY_CLAIM_HELPER=1",
			"RUSH_TEST_DISPLAY_LOCK_DIRECTORY="+directory,
		)
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := command.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		child := &childProcess{command: command, input: input, output: bufio.NewReader(output)}
		command.Stderr = &child.stderr
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	defer func() {
		for _, child := range children {
			_ = child.input.Close()
			if child.command.Process != nil {
				_ = child.command.Process.Kill()
			}
			_ = child.command.Wait()
		}
	}()

	for _, child := range children {
		if _, err := child.input.Write([]byte{1}); err != nil {
			t.Fatal(err)
		}
	}
	displays := make(map[int]bool, processCount)
	for index := range children {
		line, err := children[index].output.ReadString('\n')
		if err != nil {
			t.Fatalf("read child %d display: %v: %s", index, err, children[index].stderr.String())
		}
		display, err := strconv.Atoi(line[:len(line)-1])
		if err != nil {
			t.Fatalf("parse child %d display %q: %v", index, line, err)
		}
		if displays[display] {
			t.Fatalf("multiple Rush processes claimed display %d", display)
		}
		displays[display] = true
	}

	for index := range children {
		if err := children[index].input.Close(); err != nil {
			t.Fatal(err)
		}
		if err := children[index].command.Wait(); err != nil {
			t.Fatalf("child %d failed: %v: %s", index, err, children[index].stderr.String())
		}
		children[index].command.Process = nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("display locks remain after child exit: %v", entries)
	}
}

func TestVirtualDisplayClaimHelper(t *testing.T) {
	if os.Getenv("RUSH_TEST_DISPLAY_CLAIM_HELPER") != "1" {
		return
	}
	var start [1]byte
	if _, err := io.ReadFull(os.Stdin, start[:]); err != nil {
		t.Fatal(err)
	}
	directory := os.Getenv("RUSH_TEST_DISPLAY_LOCK_DIRECTORY")
	for display := 90; display < 190; display++ {
		release, claimed, err := claimVirtualDisplay(directory, display)
		if err != nil {
			t.Fatal(err)
		}
		if !claimed {
			continue
		}
		defer release()
		fmt.Println(display)
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	t.Fatal("no virtual display available")
}
