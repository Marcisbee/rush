//go:build !rush_wpe && !rush_lightpanda && !darwin

package rush

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// BackendName identifies the browser engine host compiled into this binary.
func BackendName() string { return "WebKitGTK" }

// SupportsHeaded reports whether this adapter can present a debugging window.
func SupportsHeaded() bool { return true }

func prepareBrowser(headed bool) (func(), error) {
	if headed {
		return func() {}, nil
	}
	return startVirtualDisplay()
}

// Doctor checks prerequisites that can be validated without starting WebKit.
func Doctor(output io.Writer) error {
	if _, err := os.Stat("/usr/bin/Xvfb"); err != nil {
		return errors.New("Xvfb was not found; on Debian/Ubuntu install xvfb")
	}
	if _, err := os.Stat("/usr/bin/xauth"); err != nil {
		return errors.New("xauth was not found; on Debian/Ubuntu install xauth")
	}
	fmt.Fprintln(output, "Go host: available")
	fmt.Fprintln(output, "headless display: Xvfb and xauth available")
	fmt.Fprintln(output, "WebKitGTK: checked when the native host starts (run `rush test benchmarks/fixtures/assertions.ts`)")
	return nil
}
