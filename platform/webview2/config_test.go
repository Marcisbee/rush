package webview2

import (
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	config, err := (Config{}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 1280 || config.Height != 720 || config.RealmPoolSize != 1 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	if config.BatchMaxMessages != 128 || config.BatchMaxBytes != 256*1024 || config.BatchFlushInterval != 2*time.Millisecond {
		t.Fatalf("unexpected batch defaults: %+v", config)
	}
}

func TestConfigRejectsNegativeLimits(t *testing.T) {
	if _, err := (Config{RealmPoolSize: -1}).normalized(); err == nil {
		t.Fatal("expected validation error")
	}
}
