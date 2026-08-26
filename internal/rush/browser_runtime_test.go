package rush

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/Marcisbee/rush/internal/runtimebundle"
)

func TestGeneratedBrowserRuntimeIsCurrent(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := runtimebundle.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, browserRuntimeJS) {
		t.Fatal("embedded browser runtime is stale; run go generate ./internal/rush")
	}
	if bytes.Contains(bytes.ToLower(browserRuntimeJS), []byte("</script")) {
		t.Fatal("embedded browser runtime contains a closing script tag and cannot be safely inlined")
	}
}
