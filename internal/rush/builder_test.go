package rush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilderReusesContextAndSeesEdits(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory, err := os.MkdirTemp(directory, ".rush-builder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureDirectory) })
	file := filepath.Join(fixtureDirectory, "suite.ts")
	if err := os.WriteFile(file, []byte("import { expect, test } from 'rush-webtest'; test('one', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	first, _, err := builder.Build(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("import { expect, test } from 'rush-webtest'; test('two', () => expect(2).toBe(2))"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _, err := builder.Build(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "two") {
		t.Fatalf("incremental rebuild did not include edit")
	}
}

func TestBuilderBatchesSuitesAndCachesUnchangedDependencyGraph(t *testing.T) {
	rushRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSH_BROWSER_MODULE", filepath.Join(rushRoot, "dist", "index.js"))
	directory := t.TempDir()
	dependency := filepath.Join(directory, "value.ts")
	first := filepath.Join(directory, "first.test.ts")
	second := filepath.Join(directory, "second.test.ts")
	if err := os.WriteFile(dependency, []byte("export const value = 'before'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first, []byte("import { value } from './value'; import { test } from 'rush-webtest'; test(value, () => {})"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("import { test } from 'rush-webtest'; test('second', () => {})"), 0600); err != nil {
		t.Fatal(err)
	}

	builder := NewBuilder()
	defer builder.Close()
	initial, initialMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if len(initial) != 2 || initialMS <= 0 || !strings.Contains(initial[0].Source, "before") || !strings.Contains(initial[1].Source, "second") {
		t.Fatalf("unexpected initial batch: count=%d build=%f", len(initial), initialMS)
	}
	cached, cachedMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if cachedMS != 0 || cached[0].Hash != initial[0].Hash || cached[1].Hash != initial[1].Hash {
		t.Fatalf("unchanged graph was not served from cache: build=%f", cachedMS)
	}

	if err := os.WriteFile(dependency, []byte("export const value = 'after dependency edit'"), 0600); err != nil {
		t.Fatal(err)
	}
	rebuilt, rebuiltMS, err := builder.BuildBatch(directory, []string{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if rebuiltMS <= 0 || rebuilt[0].Hash == initial[0].Hash || !strings.Contains(rebuilt[0].Source, "after dependency edit") {
		t.Fatalf("dependency edit did not invalidate the batch: build=%f", rebuiltMS)
	}
}

func TestBuilderReportsTypeScriptSyntaxErrors(t *testing.T) {
	directory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	fixtureDirectory, err := os.MkdirTemp(directory, ".rush-builder-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(fixtureDirectory) })
	bad := filepath.Join(fixtureDirectory, "bad.ts")
	if err := os.WriteFile(bad, []byte("const value: = 1"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	if _, _, err := builder.Build(directory, bad); err == nil {
		t.Fatal("expected syntax error")
	}
}

func TestDetectJSXImportSource(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "package.json"), []byte(`{"dependencies":{"react":"latest"},"devDependencies":{"preact":"latest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := detectJSXImportSource(directory); got != "react" {
		t.Fatalf("detected %q, want react", got)
	}
	if err := os.WriteFile(filepath.Join(directory, "package.json"), []byte(`{"devDependencies":{"preact":"latest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := detectJSXImportSource(directory); got != "preact" {
		t.Fatalf("detected %q, want preact", got)
	}
}

func TestDetectJSXImportSourceOverride(t *testing.T) {
	t.Setenv("RUSH_JSX_IMPORT_SOURCE", "solid-js")
	if got := detectJSXImportSource(t.TempDir()); got != "solid-js" {
		t.Fatalf("detected %q, want solid-js", got)
	}
}

func TestBuilderResolvesBrowserModuleForExternalSuite(t *testing.T) {
	rushRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("RUSH_BROWSER_MODULE", filepath.Join(rushRoot, "dist", "index.js"))
	externalRoot := t.TempDir()
	suite := filepath.Join(externalRoot, "external.test.ts")
	if err := os.WriteFile(suite, []byte("import { expect, test } from 'rush-webtest'; test('external', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	bundle, _, err := builder.Build(externalRoot, suite)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle, "external") {
		t.Fatal("external suite was not included in bundle")
	}
}
