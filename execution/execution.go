// Package execution keeps user-test, artifact, and reporter work in distinct
// phases so reporting cannot inflate the measured user-test time.
package execution

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/Marcisbee/rush/artifact"
	"github.com/Marcisbee/rush/command"
	"github.com/Marcisbee/rush/reporter"
	"github.com/Marcisbee/rush/result"
)

type Request struct {
	Mode     command.Mode
	Patterns []string
	Headed   bool
	Build    command.BuildConfig
}

// Runtime owns the warm native host and is the only phase counted as user
// execution. It must populate Timing.User from browser-side measurements.
type Runtime interface {
	Run(context.Context, Request) (result.Summary, error)
}

type Options struct {
	Artifacts artifact.Config
	Reporters []reporter.Output
	Stdout    io.Writer
	Now       func() time.Time
}

type Outcome struct {
	Summary result.Summary
	Errors  []error
}

func Run(ctx context.Context, runtime Runtime, request Request, options Options) (Outcome, error) {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	started := now()
	summary, runErr := runtime.Run(ctx, request)
	finished := now()
	if summary.StartedAt.IsZero() {
		summary.StartedAt = started
	}
	wall := finished.Sub(started)
	if wall > summary.Timing.User {
		summary.Timing.Runner = wall - summary.Timing.User
	}

	artifactStarted := now()
	artifactErrors := artifact.New(options.Artifacts).Capture(summary.Tests)
	summary.Timing.Artifact = now().Sub(artifactStarted)

	reporterDuration, reportErr := reporter.WriteAll(summary, options.Reporters, options.Stdout)
	summary.Timing.Reporter = reporterDuration
	errorsFound := append([]error(nil), artifactErrors...)
	if reportErr != nil {
		errorsFound = append(errorsFound, reportErr)
	}
	outcome := Outcome{Summary: summary, Errors: errorsFound}
	return outcome, errors.Join(runErr, errors.Join(errorsFound...))
}
