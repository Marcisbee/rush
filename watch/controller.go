package watch

import "context"

type Change struct {
	Paths      []string
	Invalidate bool
}

// Loop turns source-change batches into affected-suite reruns. Invalidate is
// used for configuration, plugin, and graph changes that require every suite.
func Loop(ctx context.Context, graph *Graph, changes <-chan Change, run func(context.Context, []string) error) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case change, ok := <-changes:
			if !ok {
				return nil
			}
			suites := graph.Affected(change.Paths...)
			if change.Invalidate {
				suites = graph.All()
			}
			if len(suites) == 0 {
				continue
			}
			if err := run(ctx, suites); err != nil {
				return err
			}
		}
	}
}
