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
	if err := os.WriteFile(file, []byte("import { expect, test } from '@rush/browser'; test('one', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	first, _, err := builder.Build(directory, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("import { expect, test } from '@rush/browser'; test('two', () => expect(2).toBe(2))"), 0600); err != nil {
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
