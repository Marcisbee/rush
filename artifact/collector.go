// Package artifact captures browser state after a failed test has stopped.
package artifact

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Marcisbee/rush/result"
)

type Config struct {
	Directory    string
	Screenshots  bool
	DOMSnapshots bool
}

type Collector struct {
	config Config
}

func New(config Config) *Collector { return &Collector{config: config} }

// Capture mutates failed test results with paths to successfully written
// artifacts. Collection errors are returned without discarding other artifacts.
func (c *Collector) Capture(tests []result.Test) []error {
	var errors []error
	for i := range tests {
		test := &tests[i]
		if test.Status != result.Failed || test.Failure == nil {
			continue
		}
		base := fmt.Sprintf("%03d-%s-%s", i+1, slug(test.Suite), slug(test.Name))
		if c.config.Screenshots {
			data, err := test.Failure.Screenshot()
			if err != nil {
				errors = append(errors, fmt.Errorf("capture screenshot for %s: %w", test.Name, err))
			} else if path, err := c.write(base+".png", data); err != nil {
				errors = append(errors, err)
			} else {
				test.Artifacts = append(test.Artifacts, result.Artifact{Kind: "screenshot", Path: path})
			}
		}
		if c.config.DOMSnapshots {
			data, err := test.Failure.DOMSnapshot()
			if err != nil {
				errors = append(errors, fmt.Errorf("capture DOM snapshot for %s: %w", test.Name, err))
			} else if path, err := c.write(base+".html", data); err != nil {
				errors = append(errors, err)
			} else {
				test.Artifacts = append(test.Artifacts, result.Artifact{Kind: "dom", Path: path})
			}
		}
	}
	return errors
}

func (c *Collector) write(name string, data []byte) (string, error) {
	if err := os.MkdirAll(c.config.Directory, 0o755); err != nil {
		return "", fmt.Errorf("create artifact directory: %w", err)
	}
	path := filepath.Join(c.config.Directory, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write artifact %s: %w", path, err)
	}
	return filepath.ToSlash(path), nil
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func slug(value string) string {
	value = strings.Trim(unsafeName.ReplaceAllString(value, "-"), "-.")
	if value == "" {
		return "unnamed"
	}
	if len(value) > 80 {
		return value[:80]
	}
	return value
}
