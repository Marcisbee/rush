//go:build linux

package rush

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

func claimVirtualDisplay(lockDirectory string, display int) (func(), bool, error) {
	path := filepath.Join(lockDirectory, "X"+strconv.Itoa(display)+".lock")
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			_ = os.Remove(path)
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
			_ = lock.Close()
		})
	}
	if err := lock.Truncate(0); err != nil {
		release()
		return nil, false, err
	}
	if _, err := lock.Seek(0, 0); err != nil {
		release()
		return nil, false, err
	}
	if _, err := fmt.Fprintln(lock, os.Getpid()); err != nil {
		release()
		return nil, false, err
	}
	return release, true, nil
}
