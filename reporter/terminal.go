package reporter

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Marcisbee/rush/result"
)

type Terminal struct{}

func (Terminal) Write(writer io.Writer, summary result.Summary) error {
	for _, test := range summary.Tests {
		mark := map[result.Status]string{
			result.Passed:  "PASS",
			result.Failed:  "FAIL",
			result.Skipped: "SKIP",
			result.Todo:    "TODO",
		}[test.Status]
		if _, err := fmt.Fprintf(writer, "%s %s > %s (%s)\n", mark, test.Suite, test.Name, duration(test.Duration)); err != nil {
			return err
		}
		if test.Error != "" {
			if _, err := fmt.Fprintf(writer, "  %s\n", strings.ReplaceAll(test.Error, "\n", "\n  ")); err != nil {
				return err
			}
		}
		for _, artifact := range test.Artifacts {
			if _, err := fmt.Fprintf(writer, "  %s: %s\n", artifact.Kind, artifact.Path); err != nil {
				return err
			}
		}
	}
	counts := summary.Counts()
	_, err := fmt.Fprintf(writer,
		"\n%d passed, %d failed, %d skipped, %d todo | user %s | runner %s\n",
		counts[result.Passed], counts[result.Failed], counts[result.Skipped], counts[result.Todo],
		duration(summary.Timing.User), duration(summary.Timing.Runner),
	)
	return err
}

func duration(value time.Duration) string {
	if value < time.Millisecond {
		return fmt.Sprintf("%.2fms", float64(value)/float64(time.Millisecond))
	}
	return value.Round(time.Millisecond).String()
}
