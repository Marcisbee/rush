package rush

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

type buildContext struct {
	context api.BuildContext
}

type Builder struct {
	mu       sync.Mutex
	contexts map[string]buildContext
}

func NewBuilder() *Builder {
	return &Builder{contexts: make(map[string]buildContext)}
}

func (b *Builder) Build(cwd, name string) (string, float64, error) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", 0, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	cached, ok := b.contexts[abs]
	if !ok {
		ctx, ctxErr := api.Context(api.BuildOptions{
			AbsWorkingDir:   cwd,
			EntryPoints:     []string{abs},
			Bundle:          true,
			Write:           false,
			Format:          api.FormatIIFE,
			Platform:        api.PlatformBrowser,
			Target:          api.ES2022,
			JSX:             api.JSXAutomatic,
			JSXImportSource: "preact",
			Sourcemap:       api.SourceMapInline,
			LogLevel:        api.LogLevelSilent,
		})
		if ctxErr != nil {
			return "", 0, errors.New(ctxErr.Error())
		}
		cached = buildContext{context: ctx}
		b.contexts[abs] = cached
	}

	started := time.Now()
	result := cached.context.Rebuild()
	elapsed := milliseconds(time.Since(started))
	if len(result.Errors) > 0 {
		return "", elapsed, errors.New(formatMessages(result.Errors))
	}
	if len(result.OutputFiles) != 1 {
		return "", elapsed, fmt.Errorf("esbuild returned %d output files for %s", len(result.OutputFiles), name)
	}
	return string(result.OutputFiles[0].Contents), elapsed, nil
}

func (b *Builder) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, cached := range b.contexts {
		cached.context.Dispose()
		delete(b.contexts, key)
	}
}

func formatMessages(messages []api.Message) string {
	formatted := api.FormatMessages(messages, api.FormatMessagesOptions{Kind: api.ErrorMessage, Color: false})
	result := ""
	for _, message := range formatted {
		result += message
	}
	return result
}
