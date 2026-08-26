package rush

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

type buildContext struct {
	context api.BuildContext
	inputs  map[string]fileStamp
	outputs []BuiltSuite
}

type Builder struct {
	mu             sync.Mutex
	contexts       map[string]*buildContext
	order          []string
	lastWatchFiles []string
}

const builderContextCacheLimit = 8

type fileStamp struct {
	size    int64
	modTime int64
}

// BuiltSuite is an isolated suite bundle. Hash stays stable while its complete
// dependency graph is unchanged so the browser can reuse the compiled factory.
type BuiltSuite struct {
	File   string `json:"file"`
	Source string `json:"source,omitempty"`
	Hash   string `json:"hash"`
}

type packageManifest struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func NewBuilder() *Builder {
	return &Builder{contexts: make(map[string]*buildContext)}
}

func (b *Builder) Build(cwd, name string) (string, float64, error) {
	outputs, elapsed, err := b.BuildBatch(cwd, []string{name})
	if err != nil {
		return "", elapsed, err
	}
	return outputs[0].Source, elapsed, nil
}

// BuildBatch traverses the shared dependency graph once and emits a separate
// IIFE for every suite. Separate bundles retain file-level module isolation,
// while one esbuild context avoids repeating dependency discovery per file.
func (b *Builder) BuildBatch(cwd string, names []string) ([]BuiltSuite, float64, error) {
	if len(names) == 0 {
		return nil, 0, errors.New("no suites to build")
	}
	absFiles := make([]string, len(names))
	for index, name := range names {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, 0, err
		}
		absFiles[index] = filepath.Clean(abs)
	}
	jsxImportSource := detectJSXImportSource(cwd)
	absCWD, err := filepath.Abs(cwd)
	if err != nil {
		return nil, 0, err
	}
	nodeEnvironment := os.Getenv("RUSH_NODE_ENV")
	if nodeEnvironment == "" {
		nodeEnvironment = "test"
	}
	cacheParts := append([]string{absCWD, jsxImportSource, nodeEnvironment, os.Getenv("RUSH_BROWSER_MODULE")}, absFiles...)
	cacheKey := strings.Join(cacheParts, "\x00")

	b.mu.Lock()
	defer b.mu.Unlock()

	cached, ok := b.contexts[cacheKey]
	if !ok {
		if len(b.order) >= builderContextCacheLimit {
			oldest := b.order[0]
			b.order = b.order[1:]
			b.contexts[oldest].context.Dispose()
			delete(b.contexts, oldest)
		}
		entryPoints := make([]api.EntryPoint, len(absFiles))
		for index := range absFiles {
			entryPoints[index] = api.EntryPoint{InputPath: fmt.Sprintf("rush-entry:%d", index), OutputPath: fmt.Sprintf("suite-%d", index)}
		}
		entryPlugin := api.Plugin{
			Name: "rush-suite-entries",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(api.OnResolveOptions{Filter: "^rush-entry:"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					return api.OnResolveResult{Path: strings.TrimPrefix(args.Path, "rush-entry:"), Namespace: "rush-entry"}, nil
				})
				build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "rush-entry"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					index, parseErr := strconv.Atoi(args.Path)
					if parseErr != nil || index < 0 || index >= len(absFiles) {
						return api.OnLoadResult{}, fmt.Errorf("invalid Rush entry %q", args.Path)
					}
					entry := fmt.Sprintf(
						"import * as __rushBrowser from \"rush-webtest\";\nimport %s;\nglobalThis.__rushBrowserModule = __rushBrowser;\n",
						strconv.Quote(filepath.ToSlash(absFiles[index])),
					)
					return api.OnLoadResult{Contents: &entry, Loader: api.LoaderJS, ResolveDir: cwd}, nil
				})
			},
		}
		hoistedFiles := make(map[string]bool, len(absFiles))
		for _, path := range absFiles {
			hoistedFiles[filepath.Clean(path)] = true
		}
		var mockResolutionMu sync.RWMutex
		resolvedMocksByFile := make(map[string]map[string]string, len(absFiles))
		mockedTargets := make(map[string]bool)
		hoistPlugin := api.Plugin{
			Name: "rush-hoisted-mocks",
			Setup: func(build api.PluginBuild) {
				build.OnStart(func() (api.OnStartResult, error) {
					nextByFile := make(map[string]map[string]string, len(absFiles))
					nextTargets := make(map[string]bool)
					for _, path := range absFiles {
						source, readErr := os.ReadFile(path)
						if readErr != nil {
							return api.OnStartResult{}, readErr
						}
						mocks, findErr := findMockCalls(string(source))
						if findErr != nil {
							return api.OnStartResult{}, findErr
						}
						resolved := make(map[string]string, len(mocks))
						for _, mock := range mocks {
							sourceID, sourceErr := mockModuleID(mock.arguments)
							if sourceErr != nil {
								return api.OnStartResult{}, sourceErr
							}
							result := build.Resolve(sourceID, api.ResolveOptions{
								PluginName: "rush-hoisted-mocks",
								ResolveDir: filepath.Dir(path),
								Kind:       api.ResolveJSImportStatement,
							})
							if len(result.Errors) > 0 {
								return api.OnStartResult{Errors: result.Errors}, nil
							}
							moduleID := resolvedModuleID(result)
							resolved[sourceID] = moduleID
							nextTargets[moduleID] = true
						}
						nextByFile[filepath.Clean(path)] = resolved
					}
					mockResolutionMu.Lock()
					resolvedMocksByFile = nextByFile
					mockedTargets = nextTargets
					mockResolutionMu.Unlock()
					return api.OnStartResult{}, nil
				})
				build.OnLoad(api.OnLoadOptions{Filter: `\.(?:[cm]?[jt]s|[jt]sx)$`, Namespace: "file"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					source, readErr := os.ReadFile(args.Path)
					if readErr != nil {
						return api.OnLoadResult{}, readErr
					}
					cleanPath := filepath.Clean(args.Path)
					mockResolutionMu.RLock()
					entryMocks := resolvedMocksByFile[cleanPath]
					targets := make(map[string]bool, len(mockedTargets))
					for id := range mockedTargets {
						targets[id] = true
					}
					mockResolutionMu.RUnlock()

					original := string(source)
					transformed := original
					var transformErr error
					if hoistedFiles[cleanPath] {
						transformed, transformErr = transformHoistedMocksWithIDs(original, entryMocks)
					} else if len(targets) > 0 {
						imports, findErr := findStaticImports(original)
						if findErr != nil {
							return api.OnLoadResult{}, findErr
						}
						resolvedImports := make(map[string]string)
						for _, item := range imports {
							sourceID, sourceErr := staticImportSource(original[item.start:item.end])
							if sourceErr != nil {
								return api.OnLoadResult{}, sourceErr
							}
							result := build.Resolve(sourceID, api.ResolveOptions{
								PluginName: "rush-hoisted-mocks",
								Importer:   args.Path,
								ResolveDir: filepath.Dir(args.Path),
								Kind:       api.ResolveJSImportStatement,
							})
							if len(result.Errors) > 0 {
								return api.OnLoadResult{Errors: result.Errors}, nil
							}
							moduleID := resolvedModuleID(result)
							if targets[moduleID] {
								resolvedImports[sourceID] = moduleID
							}
						}
						transformed, transformErr = transformDependencyImports(original, resolvedImports)
					}
					if transformErr != nil {
						return api.OnLoadResult{}, transformErr
					}
					if transformed == original {
						return api.OnLoadResult{}, nil
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
				build.OnResolve(api.OnResolveOptions{Filter: "^rush-webtest(?:/internal)?$"}, func(args api.OnResolveArgs) (api.OnResolveResult, error) {
					if path := os.Getenv("RUSH_BROWSER_MODULE"); path != "" && args.Path == "rush-webtest" {
						return api.OnResolveResult{Path: path}, nil
					}
					return api.OnResolveResult{Path: args.Path, Namespace: "rush-browser-runtime"}, nil
				})
				build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "rush-browser-runtime"}, func(args api.OnLoadArgs) (api.OnLoadResult, error) {
					source := "module.exports = globalThis.__rushBrowserRuntime;"
					return api.OnLoadResult{Contents: &source, Loader: api.LoaderJS}, nil
				})
			},
		}
		ctx, ctxErr := api.Context(api.BuildOptions{
			AbsWorkingDir:       cwd,
			EntryPointsAdvanced: entryPoints,
			Outdir:              filepath.Join(cwd, ".rush-build"),
			Bundle:              true,
			Write:               false,
			Format:              api.FormatESModule,
			Platform:            api.PlatformBrowser,
			Target:              api.ES2022,
			JSX:                 api.JSXAutomatic,
			JSXImportSource:     jsxImportSource,
			Sourcemap:           api.SourceMapInline,
			Metafile:            true,
			LogLevel:            api.LogLevelSilent,
			NodePaths:           []string{filepath.Join(cwd, "node_modules")},
			External:            []string{"util"},
			Plugins:             []api.Plugin{entryPlugin, browserModulePlugin, hoistPlugin},
			Banner:              map[string]string{"js": "globalThis.__rushRegistration = (async () => {"},
			Footer:              map[string]string{"js": "})();"},
			Define: map[string]string{
				"process.env.NODE_ENV": strconv.Quote(nodeEnvironment),
			},
		})
		if ctxErr != nil {
			return nil, 0, errors.New(ctxErr.Error())
		}
		cached = &buildContext{context: ctx}
		b.contexts[cacheKey] = cached
		b.order = append(b.order, cacheKey)
	}
	b.setLastWatchFiles(cached.inputs)
	b.addLastWatchFiles(absFiles...)
	if len(cached.outputs) > 0 && inputsUnchanged(cached.inputs) {
		return append([]BuiltSuite(nil), cached.outputs...), 0, nil
	}

	started := time.Now()
	result := cached.context.Rebuild()
	elapsed := milliseconds(time.Since(started))
	if len(result.Errors) > 0 {
		for _, message := range result.Errors {
			if message.Location == nil || message.Location.File == "" {
				continue
			}
			path := message.Location.File
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			b.addLastWatchFiles(filepath.Clean(path))
		}
		return nil, elapsed, errors.New(formatMessages(result.Errors))
	}
	if len(result.OutputFiles) != len(names) {
		return nil, elapsed, fmt.Errorf("esbuild returned %d output files for %d suites", len(result.OutputFiles), len(names))
	}
	byName := make(map[string][]byte, len(result.OutputFiles))
	for _, output := range result.OutputFiles {
		byName[filepath.Base(output.Path)] = output.Contents
	}
	outputs := make([]BuiltSuite, len(names))
	for index, name := range names {
		contents, exists := byName[fmt.Sprintf("suite-%d.js", index)]
		if !exists {
			return nil, elapsed, fmt.Errorf("esbuild omitted output for suite %s", name)
		}
		digest := sha256.Sum256(contents)
		outputs[index] = BuiltSuite{File: name, Source: string(contents), Hash: fmt.Sprintf("%x", digest)}
	}
	inputs, err := buildInputStamps(cwd, result.Metafile)
	if err != nil {
		return nil, elapsed, err
	}
	cached.inputs = inputs
	cached.outputs = outputs
	b.setLastWatchFiles(inputs)
	return append([]BuiltSuite(nil), outputs...), elapsed, nil
}

func resolvedModuleID(result api.ResolveResult) string {
	namespace := result.Namespace
	if namespace == "" {
		namespace = "file"
	}
	return "rush-module:" + namespace + ":" + filepath.ToSlash(result.Path) + result.Suffix
}

func (b *Builder) WatchFiles() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.lastWatchFiles...)
}

func (b *Builder) setLastWatchFiles(inputs map[string]fileStamp) {
	files := make([]string, 0, len(inputs))
	for path := range inputs {
		files = append(files, path)
	}
	sort.Strings(files)
	b.lastWatchFiles = files
}

func (b *Builder) addLastWatchFiles(paths ...string) {
	seen := make(map[string]bool, len(b.lastWatchFiles)+len(paths))
	for _, path := range b.lastWatchFiles {
		seen[path] = true
	}
	for _, path := range paths {
		if !seen[path] {
			seen[path] = true
			b.lastWatchFiles = append(b.lastWatchFiles, path)
		}
	}
	sort.Strings(b.lastWatchFiles)
}

func buildInputStamps(cwd, metafile string) (map[string]fileStamp, error) {
	var metadata struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(metafile), &metadata); err != nil {
		return nil, fmt.Errorf("decode esbuild metafile: %w", err)
	}
	inputs := make(map[string]fileStamp, len(metadata.Inputs)+2)
	for name := range metadata.Inputs {
		if strings.HasPrefix(name, "rush-entry:") || strings.HasPrefix(name, "rush-browser-runtime:") || strings.HasPrefix(name, "(disabled):") {
			continue
		}
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, filepath.FromSlash(name))
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		inputs[filepath.Clean(path)] = fileStamp{size: info.Size(), modTime: info.ModTime().UnixNano()}
	}
	for _, pattern := range []string{"package*.json", "tsconfig*.json", "jsconfig*.json"} {
		matches, _ := filepath.Glob(filepath.Join(cwd, pattern))
		for _, path := range matches {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				inputs[path] = fileStamp{size: info.Size(), modTime: info.ModTime().UnixNano()}
			}
		}
	}
	return inputs, nil
}

func inputsUnchanged(inputs map[string]fileStamp) bool {
	if len(inputs) == 0 {
		return false
	}
	for path, previous := range inputs {
		info, err := os.Stat(path)
		if err != nil || info.Size() != previous.size || info.ModTime().UnixNano() != previous.modTime {
			return false
		}
	}
	return true
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
	b.order = nil
}

func formatMessages(messages []api.Message) string {
	formatted := api.FormatMessages(messages, api.FormatMessagesOptions{Kind: api.ErrorMessage, Color: false})
	result := ""
	for _, message := range formatted {
		result += message
	}
	return result
}
