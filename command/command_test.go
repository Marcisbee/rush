package command

import (
	"bytes"
	"reflect"
	"testing"
)

func TestParseCompleteSurface(t *testing.T) {
	config, err := Parse([]string{
		"watch", "--headed", "--reporter=terminal,junit,json", "--output-file=junit=reports/junit.xml",
		"--output-file=json=reports/results.json", "--jsx=preserve", "--jsx-import-source=preact",
		"--alias=@app=./src", "--transform=react=./transform.js", "--plugin=./plugin.js",
		"src/**/*.test.tsx",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != Watch || !config.Headed {
		t.Fatalf("unexpected mode: %#v", config)
	}
	if !reflect.DeepEqual(config.Patterns, []string{"src/**/*.test.tsx"}) {
		t.Fatalf("patterns = %#v", config.Patterns)
	}
	if config.Build.JSX != JSXPreserve || config.Build.JSXImportSource != "preact" {
		t.Fatalf("build = %#v", config.Build)
	}
	if config.Build.Aliases["@app"] != "./src" || config.Build.Transforms["react"] != "./transform.js" {
		t.Fatalf("build mappings = %#v", config.Build)
	}
	if !reflect.DeepEqual(config.Build.Plugins, []string{"./plugin.js"}) {
		t.Fatalf("plugins = %#v", config.Build.Plugins)
	}
	if config.Reporters[1].OutputFile != "reports/junit.xml" || config.Reporters[2].OutputFile != "reports/results.json" {
		t.Fatalf("reporters = %#v", config.Reporters)
	}
}

func TestDebugAndAliases(t *testing.T) {
	config, err := Parse([]string{"run", "--debug", "--reporter=tap", "--reporter=github"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != Debug || !config.Headed {
		t.Fatalf("debug did not imply headed: %#v", config)
	}
	if got := []string{config.Reporters[0].Name, config.Reporters[1].Name}; !reflect.DeepEqual(got, []string{"tap", "github"}) {
		t.Fatalf("reporters = %#v", got)
	}
}

func TestParseRejectsInvalidConfiguration(t *testing.T) {
	tests := [][]string{
		{"--jsx=unknown"},
		{"--alias=missing-value"},
		{"--reporter=unknown"},
		{"--output-file=json=out.json"},
		{"watch", "--debug"},
		{"debug", "--watch"},
	}
	for _, args := range tests {
		if _, err := Parse(args, &bytes.Buffer{}); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", args)
		}
	}
}
