package reporter

import (
	"encoding/json"
	"io"

	"github.com/Marcisbee/rush/result"
)

type JSON struct{}

func (JSON) Write(writer io.Writer, summary result.Summary) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}
