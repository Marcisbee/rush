package rush

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

type packageManifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
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
	jsxImportSource := detectJSXImportSource(cwd)
	nodeEnvironment := os.Getenv("RUSH_NODE_ENV")
	if nodeEnvironment == "" {
		nodeEnvironment = "test"
	}
	cacheKey := strings.Join([]string{abs, jsxImportSource, nodeEnvironment}, "\x00")

	b.mu.Lock()
	defer b.mu.Unlock()

	cached, ok := b.contexts[cacheKey]
	if !ok {
		entry := fmt.Sprintf(
			"import * as __rushBrowser from \"@rush/browser\";\nimport %s;\nglobalThis.__rushBrowserModule = __rushBrowser;\n",
			strconv.Quote(filepath.ToSlash(abs)),
		)
		hoistPlugin := api.Plugin{
			Name: "rush-hoisted-mocks",
			Setup: func(build api.PluginBuild) {
				build.OnLoad(api.OnLoadOptions{Filter: "^" + regexp.QuoteMeta(filepath.ToSlash(abs)) + "$", Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					source, readErr := os.ReadFile(args.Path)
					if readErr != nil {
						return api.OnLoadResult{}, readErr
					}
					transformed, transformErr := transformHoistedMocks(string(source))
					if transformErr != nil {
						return api.OnLoadResult{}, transformErr
					}
					loader := loaderForPath(args.Path)
					return api.OnLoadResult{
						Contents:   &transformed,
						Loader:     loader,
						ResolveDir: filepath.Dir(args.Path),
						WatchFiles: []string{args.Path},
					}, nil
				})
			},
		}
		browserModulePlugin := api.Plugin{
			Name: "rush-browser-module",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: "^@rush/browser$"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					if path := browserModulePath(cwd); path != "" {
						return api.OnResolveResult{Path: path}, nil
					}
					return api.OnResolveResult{}, nil
				})
			},
		}
		ctx, ctxErr := api.Context(api.BuildOptions{
			AbsWorkingDir: cwd,
			Stdin: &api.StdinOptions{
				Contents:   entry,
				ResolveDir: cwd,
				Sourcefile: "rush-native-entry.js",
				Loader:     api.LoaderJS,
			},
			Bundle:          true,
			Write:           false,
			Format:          api.FormatIIFE,
			Platform:        api.PlatformBrowser,
			Target:          api.ES2022,
			JSX:             api.JSXAutomatic,
			JSXImportSource: jsxImportSource,
			Sourcemap:       api.SourceMapInline,
			LogLevel:        api.LogLevelSilent,
			NodePaths:       []string{filepath.Join(cwd, "node_modules")},
			External:        []string{"util"},
			Plugins:         []api.Plugin{browserModulePlugin, hoistPlugin},
			Define: map[string]string{
				"process.env.NODE_ENV": strconv.Quote(nodeEnvironment),
			},
		})
		if ctxErr != nil {
			return "", 0, errors.New(ctxErr.Error())
		}
		cached = buildContext{context: ctx}
		b.contexts[cacheKey] = cached
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

func browserModulePath(cwd string) string {
	candidates := []string{os.Getenv("RUSH_BROWSER_MODULE")}
	candidates = append(candidates,
		filepath.Join(cwd, "node_modules", "@rush", "browser", "dist", "index.js"),
		filepath.Join(cwd, "dist", "index.js"),
	)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "dist", "index.js"))
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(absolute); err == nil && !info.IsDir() {
			return absolute
		}
	}
	return ""
}

func detectJSXImportSource(cwd string) string {
	if configured := os.Getenv("RUSH_JSX_IMPORT_SOURCE"); configured != "" {
		return configured
	}
	contents, err := os.ReadFile(filepath.Join(cwd, "package.json"))
	if err != nil {
		return "react"
	}
	var manifest packageManifest
	if json.Unmarshal(contents, &manifest) != nil {
		return "react"
	}
	has := func(name string) bool {
		_, dependency := manifest.Dependencies[name]
		_, devDependency := manifest.DevDependencies[name]
		return dependency || devDependency
	}
	if has("react") {
		return "react"
	}
	if has("preact") {
		return "preact"
	}
	return "react"
}

func loaderForPath(path string) api.Loader {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".mjs", ".cjs":
		return api.LoaderJS
	case ".jsx":
		return api.LoaderJSX
	case ".tsx":
		return api.LoaderTSX
	default:
		return api.LoaderTS
	}
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
