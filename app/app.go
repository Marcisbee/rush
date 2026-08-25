// Package app connects the CLI contract to a native runtime adapter.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/Marcisbee/rush/artifact"
	"github.com/Marcisbee/rush/command"
	"github.com/Marcisbee/rush/execution"
	"github.com/Marcisbee/rush/reporter"
	"github.com/Marcisbee/rush/watch"
)

var ErrTestsFailed = errors.New("tests failed")

type App struct {
	Runtime execution.Runtime
	Graph   *watch.Graph
	Changes <-chan watch.Change
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run parses one command and executes it. Watch mode performs an initial run,
// then consumes native-host change batches until the context or channel closes.
func (app App) Run(ctx context.Context, args []string) error {
	if app.Runtime == nil {
		return errors.New("Rush native runtime is not configured")
	}
	stderr := app.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	config, err := command.Parse(args, stderr)
	if err != nil {
		return err
	}

	run := func(ctx context.Context, patterns []string) (bool, error) {
		request := execution.Request{Mode: config.Mode, Patterns: patterns, Headed: config.Headed, Build: config.Build}
		outcome, err := execution.Run(ctx, app.Runtime, request, execution.Options{
			Artifacts: artifact.Config{
				Directory:    config.Artifacts.Directory,
				Screenshots:  config.Artifacts.Screenshots,
				DOMSnapshots: config.Artifacts.DOMSnapshots,
			},
			Reporters: reporterOutputs(config.Reporters),
			Stdout:    app.Stdout,
		})
		return outcome.Summary.Failed(), err
	}

	failed, err := run(ctx, config.Patterns)
	if err != nil {
		return err
	}
	if config.Mode != command.Watch {
		if failed {
			return ErrTestsFailed
		}
		return nil
	}
	if app.Graph == nil || app.Changes == nil {
		return errors.New("watch mode requires a dependency graph and change source")
	}
	return watch.Loop(ctx, app.Graph, app.Changes, func(ctx context.Context, suites []string) error {
		_, err := run(ctx, suites)
		return err
	})
}

// ExitCode maps an App result to the process contract used by platform mains.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, ErrTestsFailed) {
		return 1
	}
	if errors.Is(err, context.Canceled) {
		return 130
	}
	return 2
}

func reporterOutputs(configs []command.Reporter) []reporter.Output {
	outputs := make([]reporter.Output, 0, len(configs))
	for _, config := range configs {
		outputs = append(outputs, reporter.Output{Name: config.Name, Path: config.OutputFile})
	}
	return outputs
}

func PrintError(writer io.Writer, err error) {
	if writer != nil && err != nil && !errors.Is(err, ErrTestsFailed) && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(writer, "rush:", err)
	}
}
