//go:build rush_lightpanda && linux

package rush

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func BackendName() string { return "Lightpanda" }

func SupportsHeaded() bool { return false }

func prepareBrowser(headed bool) (func(), error) {
	if headed {
		return nil, errors.New("Lightpanda is headless-only")
	}
	return func() {}, nil
}

func Doctor(output io.Writer) error {
	executable := os.Getenv("RUSH_LIGHTPANDA_PATH")
	if executable == "" {
		executable = "lightpanda"
	}
	path, err := exec.LookPath(executable)
	if err != nil {
		return errors.New("Lightpanda was not found; set RUSH_LIGHTPANDA_PATH to the nightly executable")
	}
	version, err := exec.Command(path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("run Lightpanda version: %w", err)
	}
	fmt.Fprintf(output, "Lightpanda: %s", version)
	return nil
}
