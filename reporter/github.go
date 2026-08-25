package reporter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Marcisbee/rush/result"
)

type GitHub struct{}

func (GitHub) Write(writer io.Writer, summary result.Summary) error {
	for _, test := range summary.Tests {
		if test.Status != result.Failed {
			continue
		}
		properties := make([]string, 0, 4)
		if test.Location.File != "" {
			properties = append(properties, "file="+escapeProperty(test.Location.File))
		}
		if test.Location.Line > 0 {
			properties = append(properties, "line="+strconv.Itoa(test.Location.Line))
		}
		if test.Location.Column > 0 {
			properties = append(properties, "col="+strconv.Itoa(test.Location.Column))
		}
		properties = append(properties, "title="+escapeProperty(test.Name))
		if _, err := fmt.Fprintf(writer, "::error %s::%s\n", strings.Join(properties, ","), escapeData(test.Error)); err != nil {
			return err
		}
	}
	return nil
}

func escapeData(value string) string {
	value = strings.ReplaceAll(value, "%", "%25")
	value = strings.ReplaceAll(value, "\r", "%0D")
	return strings.ReplaceAll(value, "\n", "%0A")
}

func escapeProperty(value string) string {
	value = escapeData(value)
	value = strings.ReplaceAll(value, ":", "%3A")
	return strings.ReplaceAll(value, ",", "%2C")
}
