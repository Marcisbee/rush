// Package result contains the adapter-independent result protocol.
package result

import "time"

type Status string

const (
	Passed  Status = "passed"
	Failed  Status = "failed"
	Skipped Status = "skipped"
	Todo    Status = "todo"
)

type Location struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Artifact struct {
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Test struct {
	Suite     string        `json:"suite"`
	Name      string        `json:"name"`
	Status    Status        `json:"status"`
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	Location  Location      `json:"location,omitempty"`
	Artifacts []Artifact    `json:"artifacts,omitempty"`
	Failure   FailureSource `json:"-"`
}

// FailureSource is supplied by a browser adapter only for failed tests.
type FailureSource interface {
	Screenshot() ([]byte, error)
	DOMSnapshot() ([]byte, error)
}

type Timing struct {
	User     time.Duration `json:"user"`
	Runner   time.Duration `json:"runner"`
	Artifact time.Duration `json:"artifact"`
	Reporter time.Duration `json:"reporter"`
}

type Summary struct {
	StartedAt time.Time `json:"startedAt"`
	Tests     []Test    `json:"tests"`
	Timing    Timing    `json:"timing"`
}

func (s Summary) Counts() map[Status]int {
	counts := map[Status]int{Passed: 0, Failed: 0, Skipped: 0, Todo: 0}
	for _, test := range s.Tests {
		counts[test.Status]++
	}
	return counts
}

func (s Summary) Failed() bool { return s.Counts()[Failed] > 0 }
