package rush

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuilderReusesContextAndSeesEdits(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "suite.ts")
	if err := os.WriteFile(file, []byte("test('one', () => expect(1).toBe(1))"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	first, _, err := builder.Build(directory, "suite.ts")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("test('two', () => expect(2).toBe(2))"), 0600); err != nil {
		t.Fatal(err)
	}
	second, _, err := builder.Build(directory, "suite.ts")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "two") {
		t.Fatalf("incremental rebuild did not include edit")
	}
}

func TestBuilderReportsTypeScriptSyntaxErrors(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "bad.ts"), []byte("const value: = 1"), 0600); err != nil {
		t.Fatal(err)
	}
	builder := NewBuilder()
	defer builder.Close()
	if _, _, err := builder.Build(directory, "bad.ts"); err == nil {
		t.Fatal("expected syntax error")
	}
}
