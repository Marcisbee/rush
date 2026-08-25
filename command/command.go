// Package command defines Rush's deliberately small command surface.
package command

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

type Mode string

const (
	Run   Mode = "run"
	Watch Mode = "watch"
	Debug Mode = "debug"
)

type JSXMode string

const (
	JSXAutomatic JSXMode = "automatic"
	JSXTransform JSXMode = "transform"
	JSXPreserve  JSXMode = "preserve"
)

// BuildConfig is passed unchanged to the incremental esbuild host.
type BuildConfig struct {
	JSX             JSXMode
	JSXImportSource string
	Aliases         map[string]string
	Transforms      map[string]string
	Plugins         []string
}

type ArtifactConfig struct {
	Directory    string
	Screenshots  bool
	DOMSnapshots bool
}

type Reporter struct {
	Name       string
	OutputFile string
}

type Config struct {
	Mode      Mode
	Patterns  []string
	Headed    bool
	Reporters []Reporter
	Build     BuildConfig
	Artifacts ArtifactConfig
}

type stringList []string

func (v *stringList) String() string { return strings.Join(*v, ",") }
func (v *stringList) Set(value string) error {
	for item := range strings.SplitSeq(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			*v = append(*v, item)
		}
	}
	return nil
}

// Parse accepts `rush [run|watch|debug] [options] [patterns...]`. The --watch,
// --headed, and --debug aliases keep the common single-command form concise.
func Parse(args []string, stderr io.Writer) (Config, error) {
	config := Config{
		Mode:  Run,
		Build: BuildConfig{JSX: JSXAutomatic},
		Artifacts: ArtifactConfig{
			Directory:    ".rush/artifacts",
			Screenshots:  true,
			DOMSnapshots: true,
		},
	}

	if len(args) > 0 {
		switch Mode(args[0]) {
		case Run, Watch, Debug:
			config.Mode = Mode(args[0])
			args = args[1:]
		}
	}

	set := flag.NewFlagSet("rush", flag.ContinueOnError)
	set.SetOutput(stderr)
	set.Usage = func() {
		fmt.Fprintln(stderr, "usage: rush [run|watch|debug] [options] [patterns...]")
		set.PrintDefaults()
	}
	var (
		watchFlag      bool
		debugFlag      bool
		reporterNames  stringList
		reporterOutput stringList
		aliases        stringList
		transforms     stringList
		plugins        stringList
		jsx            = string(config.Build.JSX)
	)
	set.BoolVar(&watchFlag, "watch", false, "rerun suites affected by changed files")
	set.BoolVar(&watchFlag, "w", false, "alias for --watch")
	set.BoolVar(&config.Headed, "headed", false, "show the native WebView")
	set.BoolVar(&debugFlag, "debug", false, "show the WebView and developer tools")
	set.Var(&reporterNames, "reporter", "reporter name(s): terminal, junit, tap, json, github")
	set.Var(&reporterOutput, "output-file", "reporter=path (repeatable)")
	set.StringVar(&jsx, "jsx", jsx, "JSX mode: automatic, transform, preserve")
	set.StringVar(&config.Build.JSXImportSource, "jsx-import-source", "", "automatic JSX import source")
	set.Var(&aliases, "alias", "module=path (repeatable)")
	set.Var(&transforms, "transform", "name=module (repeatable)")
	set.Var(&plugins, "plugin", "esbuild plugin module (repeatable)")
	set.StringVar(&config.Artifacts.Directory, "artifacts-dir", config.Artifacts.Directory, "failure artifact directory")
	set.BoolVar(&config.Artifacts.Screenshots, "screenshots", config.Artifacts.Screenshots, "capture failure screenshots")
	set.BoolVar(&config.Artifacts.DOMSnapshots, "dom-snapshots", config.Artifacts.DOMSnapshots, "capture failure DOM snapshots")
	if err := set.Parse(args); err != nil {
		return Config{}, err
	}

	if watchFlag && debugFlag || watchFlag && config.Mode == Debug || debugFlag && config.Mode == Watch {
		return Config{}, errors.New("watch and debug modes are mutually exclusive; use watch --headed for visible reruns")
	}
	if watchFlag {
		config.Mode = Watch
	}
	if debugFlag {
		config.Mode = Debug
	}
	if config.Mode == Debug {
		config.Headed = true
	}
	config.Patterns = set.Args()

	config.Build.JSX = JSXMode(jsx)
	if !slices.Contains([]JSXMode{JSXAutomatic, JSXTransform, JSXPreserve}, config.Build.JSX) {
		return Config{}, fmt.Errorf("unsupported JSX mode %q", jsx)
	}
	var err error
	if config.Build.Aliases, err = keyValues("alias", aliases); err != nil {
		return Config{}, err
	}
	if config.Build.Transforms, err = keyValues("transform", transforms); err != nil {
		return Config{}, err
	}
	config.Build.Plugins = append([]string(nil), plugins...)

	outputs, err := keyValues("output-file", reporterOutput)
	if err != nil {
		return Config{}, err
	}
	if len(reporterNames) == 0 {
		reporterNames = []string{"terminal"}
	}
	seen := map[string]bool{}
	for _, name := range reporterNames {
		if !slices.Contains([]string{"terminal", "junit", "tap", "json", "github"}, name) {
			return Config{}, fmt.Errorf("unknown reporter %q", name)
		}
		if !seen[name] {
			config.Reporters = append(config.Reporters, Reporter{Name: name, OutputFile: outputs[name]})
			seen[name] = true
		}
	}
	for name := range outputs {
		if !seen[name] {
			return Config{}, fmt.Errorf("output-file configured for inactive reporter %q", name)
		}
	}
	return config, nil
}

func keyValues(label string, values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		key, item, ok := strings.Cut(value, "=")
		key, item = strings.TrimSpace(key), strings.TrimSpace(item)
		if !ok || key == "" || item == "" {
			return nil, errors.New(label + " must use non-empty key=value syntax")
		}
		result[key] = item
	}
	return result, nil
}
