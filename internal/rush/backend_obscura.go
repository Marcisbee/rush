//go:build linux && rush_obscura

package rush

import (
	"errors"
	"fmt"
	"io"
)

func BackendName() string { return "Obscura no-render" }

func SupportsHeaded() bool { return false }

func prepareBrowser(headed bool) (func(), error) {
	if headed {
		return nil, errors.New("the Obscura adapter is headless-only")
	}
	if _, err := obscuraBinary(); err != nil {
		return nil, err
	}
	return func() {}, nil
}

func Doctor(output io.Writer) error {
	binary, err := obscuraBinary()
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "Go host: available")
	fmt.Fprintf(output, "Obscura no-render: %s\n", binary)
	fmt.Fprintln(output, "headless execution: CDP over one Obscura process per browser realm")
	return nil
}
