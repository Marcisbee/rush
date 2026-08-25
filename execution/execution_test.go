package execution

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Marcisbee/rush/artifact"
	"github.com/Marcisbee/rush/reporter"
	"github.com/Marcisbee/rush/result"
)

type runtimeFunc func(context.Context, Request) (result.Summary, error)

func (function runtimeFunc) Run(ctx context.Context, request Request) (result.Summary, error) {
	return function(ctx, request)
}

type failingPage struct {
	events *[]string
}

func (page failingPage) Screenshot() ([]byte, error) {
	*page.events = append(*page.events, "screenshot")
	return []byte("png"), nil
}
func (page failingPage) DOMSnapshot() ([]byte, error) {
	*page.events = append(*page.events, "dom")
	return []byte("<body>failure</body>"), nil
}

func TestRunSeparatesExecutionArtifactsAndReporting(t *testing.T) {
	events := []string{}
	runtime := runtimeFunc(func(_ context.Context, _ Request) (result.Summary, error) {
		events = append(events, "runtime")
		return result.Summary{
			Tests:  []result.Test{{Suite: "suite", Name: "failure", Status: result.Failed, Error: "broken", Failure: failingPage{events: &events}}},
			Timing: result.Timing{User: 10 * time.Millisecond},
		}, nil
	})
	clock := []time.Time{
		time.Unix(0, 0),
		time.Unix(0, int64(100*time.Millisecond)),
		time.Unix(0, int64(100*time.Millisecond)),
		time.Unix(0, int64(110*time.Millisecond)),
	}
	next := 0
	var output bytes.Buffer
	outcome, err := Run(context.Background(), runtime, Request{}, Options{
		Artifacts: artifact.Config{Directory: t.TempDir(), Screenshots: true, DOMSnapshots: true},
		Reporters: []reporter.Output{{Name: "terminal"}},
		Stdout:    &output,
		Now: func() time.Time {
			value := clock[next]
			next++
			return value
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Summary.Timing.User != 10*time.Millisecond || outcome.Summary.Timing.Runner != 90*time.Millisecond || outcome.Summary.Timing.Artifact != 10*time.Millisecond || outcome.Summary.Timing.Reporter <= 0 {
		t.Fatalf("timing = %#v", outcome.Summary.Timing)
	}
	if got := len(outcome.Summary.Tests[0].Artifacts); got != 2 {
		t.Fatalf("artifact count = %d", got)
	}
	if len(events) != 3 || events[0] != "runtime" || events[1] != "screenshot" || events[2] != "dom" {
		t.Fatalf("events = %#v", events)
	}
	if !bytes.Contains(output.Bytes(), []byte("FAIL suite > failure")) || !bytes.Contains(output.Bytes(), []byte("screenshot:")) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunReturnsRuntimeAndPostProcessingErrors(t *testing.T) {
	runError := errors.New("bridge closed")
	runtime := runtimeFunc(func(context.Context, Request) (result.Summary, error) {
		return result.Summary{}, runError
	})
	_, err := Run(context.Background(), runtime, Request{}, Options{Reporters: []reporter.Output{{Name: "missing"}}, Stdout: &bytes.Buffer{}})
	if !errors.Is(err, runError) || err == nil || !bytes.Contains([]byte(err.Error()), []byte("unknown reporter")) {
		t.Fatalf("error = %v", err)
	}
}
