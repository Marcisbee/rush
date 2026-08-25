// Package reporter renders Rush's stable result protocol.
package reporter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Marcisbee/rush/result"
)

type Reporter interface {
	Write(io.Writer, result.Summary) error
}

type Output struct {
	Name string
	Path string
}

func New(name string) (Reporter, error) {
	switch name {
	case "terminal":
		return Terminal{}, nil
	case "junit":
		return JUnit{}, nil
	case "tap":
		return TAP{}, nil
	case "json":
		return JSON{}, nil
	case "github":
		return GitHub{}, nil
	default:
		return nil, fmt.Errorf("unknown reporter %q", name)
	}
}

// WriteAll runs only after user execution and reports its own elapsed duration.
// Stdout is never closed; configured files are created with parent directories.
func WriteAll(summary result.Summary, outputs []Output, stdout io.Writer) (time.Duration, error) {
	started := time.Now()
	var errs []error
	for _, output := range outputs {
		reporter, err := New(output.Name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		writer := stdout
		var file *os.File
		if output.Path != "" {
			file, err = createFile(output.Path)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			writer = file
		}
		if err := reporter.Write(writer, summary); err != nil {
			errs = append(errs, fmt.Errorf("write %s report: %w", output.Name, err))
		}
		if file != nil {
			if err := file.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close %s: %w", output.Path, err))
			}
		}
	}
	return time.Since(started), errors.Join(errs...)
}

func createFile(path string) (*os.File, error) {
	directory := filepathDir(path)
	if directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("create report directory: %w", err)
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create report %s: %w", path, err)
	}
	return file, nil
}
