package webview2

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// Mode controls how the native WebView2 controller is hosted.
type Mode uint8

const (
	// ModeHidden keeps the native host window hidden for normal test runs.
	ModeHidden Mode = iota
	// ModeDebug shows the host window and opens the WebView2 DevTools window.
	ModeDebug
)

// Config configures a persistent WebView2 host and its realm pool.
type Config struct {
	Mode                    Mode
	Width                   int
	Height                  int
	RealmPoolSize           int
	UserDataDir             string
	BrowserExecutableFolder string
	ArtifactDir             string
	BatchMaxMessages        int
	BatchMaxBytes           int
	BatchFlushInterval      time.Duration
}

func (c Config) normalized() (Config, error) {
	if c.Mode != ModeHidden && c.Mode != ModeDebug {
		return Config{}, fmt.Errorf("webview2: unsupported mode %d", c.Mode)
	}
	if c.Width < 0 || c.Height < 0 || c.RealmPoolSize < 0 || c.BatchMaxMessages < 0 || c.BatchMaxBytes < 0 || c.BatchFlushInterval < 0 {
		return Config{}, errors.New("webview2: dimensions, pool and batching limits cannot be negative")
	}
	if c.Width == 0 {
		c.Width = 1280
	}
	if c.Height == 0 {
		c.Height = 720
	}
	if c.RealmPoolSize == 0 {
		c.RealmPoolSize = 1
	}
	if c.BatchMaxMessages == 0 {
		c.BatchMaxMessages = 128
	}
	if c.BatchMaxBytes == 0 {
		c.BatchMaxBytes = 256 * 1024
	}
	if c.BatchFlushInterval == 0 {
		c.BatchFlushInterval = 2 * time.Millisecond
	}
	if c.UserDataDir == "" {
		c.UserDataDir = filepath.Join(".rush", "webview2")
	}
	if c.ArtifactDir == "" {
		c.ArtifactDir = filepath.Join(".rush", "artifacts")
	}
	return c, nil
}
