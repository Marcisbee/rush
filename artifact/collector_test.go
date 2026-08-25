package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Marcisbee/rush/result"
)

type source struct {
	screenshot []byte
	dom        []byte
	domErr     error
}

func (s source) Screenshot() ([]byte, error)  { return s.screenshot, nil }
func (s source) DOMSnapshot() ([]byte, error) { return s.dom, s.domErr }

func TestCaptureFailureArtifacts(t *testing.T) {
	directory := t.TempDir()
	tests := []result.Test{{Suite: "ui/card.test.ts", Name: "renders / a card", Status: result.Failed, Failure: source{screenshot: []byte("png"), dom: []byte("<main>failed</main>")}}}
	errs := New(Config{Directory: directory, Screenshots: true, DOMSnapshots: true}).Capture(tests)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if len(tests[0].Artifacts) != 2 {
		t.Fatalf("artifacts = %#v", tests[0].Artifacts)
	}
	for _, artifact := range tests[0].Artifacts {
		if _, err := os.Stat(filepath.FromSlash(artifact.Path)); err != nil {
			t.Errorf("artifact %s: %v", artifact.Path, err)
		}
	}
}

func TestCaptureKeepsSuccessfulArtifactsWhenOneFails(t *testing.T) {
	tests := []result.Test{{Suite: "suite", Name: "test", Status: result.Failed, Failure: source{screenshot: []byte("png"), domErr: errors.New("page closed")}}}
	errs := New(Config{Directory: t.TempDir(), Screenshots: true, DOMSnapshots: true}).Capture(tests)
	if len(errs) != 1 || len(tests[0].Artifacts) != 1 || tests[0].Artifacts[0].Kind != "screenshot" {
		t.Fatalf("errors=%v artifacts=%#v", errs, tests[0].Artifacts)
	}
}
