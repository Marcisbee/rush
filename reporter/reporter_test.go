package reporter

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Marcisbee/rush/result"
)

func fixture() result.Summary {
	return result.Summary{
		StartedAt: time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
		Tests: []result.Test{
			{Suite: "card.test.ts", Name: "renders", Status: result.Passed, Duration: 2 * time.Millisecond},
			{Suite: "card.test.ts", Name: "reports failure", Status: result.Failed, Duration: 3 * time.Millisecond, Error: "expected: yes\nreceived: no", Location: result.Location{File: "src/card,test.ts", Line: 8, Column: 4}, Artifacts: []result.Artifact{{Kind: "screenshot", Path: "artifacts/failure.png"}}},
			{Suite: "later.test.ts", Name: "pending", Status: result.Todo},
		},
		Timing: result.Timing{User: 5 * time.Millisecond, Runner: time.Millisecond},
	}
}

func TestAllReporterFormats(t *testing.T) {
	tests := []struct {
		name     string
		contains []string
	}{
		{"terminal", []string{"PASS card.test.ts > renders", "1 passed, 1 failed", "screenshot: artifacts/failure.png"}},
		{"tap", []string{"TAP version 13", "1..3", "not ok 2", "# TODO"}},
		{"github", []string{"::error file=src/card%2Ctest.ts,line=8,col=4,title=reports failure::expected: yes%0Areceived: no"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reporter, err := New(test.name)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := reporter.Write(&output, fixture()); err != nil {
				t.Fatal(err)
			}
			for _, expected := range test.contains {
				if !strings.Contains(output.String(), expected) {
					t.Errorf("output %q does not contain %q", output.String(), expected)
				}
			}
		})
	}
}

func TestJSONIsMachineReadable(t *testing.T) {
	var output bytes.Buffer
	if err := (JSON{}).Write(&output, fixture()); err != nil {
		t.Fatal(err)
	}
	var decoded result.Summary
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Tests) != 3 || decoded.Tests[1].Status != result.Failed {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestJUnitIsValidAndGrouped(t *testing.T) {
	var output bytes.Buffer
	if err := (JUnit{}).Write(&output, fixture()); err != nil {
		t.Fatal(err)
	}
	var decoded xmlSuites
	if err := xml.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Tests != 3 || decoded.Failures != 1 || decoded.Skipped != 1 || len(decoded.Suites) != 2 {
		t.Fatalf("decoded = %#v", decoded)
	}
	if decoded.Suites[0].Tests != 2 || decoded.Suites[0].Failures != 1 {
		t.Fatalf("suite = %#v", decoded.Suites[0])
	}
}

func TestWriteAllRoutesConfiguredOutputFiles(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "reports", "junit.xml")
	var stdout bytes.Buffer
	duration, err := WriteAll(fixture(), []Output{{Name: "terminal"}, {Name: "junit", Path: path}}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	if duration <= 0 || !strings.Contains(stdout.String(), "1 passed") {
		t.Fatalf("duration=%v stdout=%q", duration, stdout.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("<testsuites")) {
		t.Fatalf("junit = %q", data)
	}
}
