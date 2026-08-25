package reporter

import (
	"fmt"
	"io"
	"strings"

	"github.com/Marcisbee/rush/result"
)

type TAP struct{}

func (TAP) Write(writer io.Writer, summary result.Summary) error {
	if _, err := fmt.Fprintf(writer, "TAP version 13\n1..%d\n", len(summary.Tests)); err != nil {
		return err
	}
	for index, test := range summary.Tests {
		name := strings.ReplaceAll(test.Suite+" > "+test.Name, "#", "\\#")
		line := "ok"
		directive := ""
		switch test.Status {
		case result.Failed:
			line = "not ok"
		case result.Skipped:
			directive = " # SKIP"
		case result.Todo:
			directive = " # TODO"
		}
		if _, err := fmt.Fprintf(writer, "%s %d - %s%s\n", line, index+1, name, directive); err != nil {
			return err
		}
		if test.Status == result.Failed && test.Error != "" {
			if _, err := fmt.Fprintf(writer, "  ---\n  message: %q\n  ...\n", test.Error); err != nil {
				return err
			}
		}
	}
	return nil
}
