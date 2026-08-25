package app

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Marcisbee/rush/command"
	"github.com/Marcisbee/rush/execution"
	"github.com/Marcisbee/rush/result"
	"github.com/Marcisbee/rush/watch"
)

type recordingRuntime struct {
	requests []execution.Request
	failed   bool
}

func (runtime *recordingRuntime) Run(_ context.Context, request execution.Request) (result.Summary, error) {
	runtime.requests = append(runtime.requests, request)
	status := result.Passed
	if runtime.failed {
		status = result.Failed
	}
	return result.Summary{Tests: []result.Test{{Suite: "suite", Name: "test", Status: status}}}, nil
}

func TestRunConnectsCommandToRuntime(t *testing.T) {
	runtime := &recordingRuntime{}
	var output bytes.Buffer
	err := (App{Runtime: runtime, Stdout: &output}).Run(context.Background(), []string{"debug", "--reporter=json", "src/card.test.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.requests) != 1 {
		t.Fatalf("requests = %#v", runtime.requests)
	}
	request := runtime.requests[0]
	if request.Mode != command.Debug || !request.Headed || !reflect.DeepEqual(request.Patterns, []string{"src/card.test.ts"}) {
		t.Fatalf("request = %#v", request)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"status": "passed"`)) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestRunWatchRerunsAffectedSuites(t *testing.T) {
	runtime := &recordingRuntime{}
	graph := watch.New()
	graph.Suite("tests/card.test.ts")
	graph.Suite("tests/menu.test.ts")
	graph.Add("tests/card.test.ts", "src/card.ts")
	changes := make(chan watch.Change, 1)
	changes <- watch.Change{Paths: []string{"src/card.ts"}}
	close(changes)
	err := (App{Runtime: runtime, Graph: graph, Changes: changes, Stdout: &bytes.Buffer{}}).Run(context.Background(), []string{"watch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.requests) != 2 || !reflect.DeepEqual(runtime.requests[1].Patterns, []string{"tests/card.test.ts"}) {
		t.Fatalf("requests = %#v", runtime.requests)
	}
}

func TestExitCodes(t *testing.T) {
	if ExitCode(nil) != 0 || ExitCode(ErrTestsFailed) != 1 || ExitCode(context.Canceled) != 130 || ExitCode(errors.New("broken")) != 2 {
		t.Fatal("unexpected exit-code mapping")
	}
	runtime := &recordingRuntime{failed: true}
	err := (App{Runtime: runtime, Stdout: &bytes.Buffer{}}).Run(context.Background(), nil)
	if !errors.Is(err, ErrTestsFailed) {
		t.Fatalf("error = %v", err)
	}
}
