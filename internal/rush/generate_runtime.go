//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/Marcisbee/rush/internal/runtimebundle"
)

func main() {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("locate runtime generator")
	}
	packageDirectory := filepath.Dir(sourceFile)
	root := filepath.Clean(filepath.Join(packageDirectory, "..", ".."))
	output, err := runtimebundle.Build(root)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(packageDirectory, "browser_runtime.generated.js"), output, 0o644); err != nil {
		panic(err)
	}
}
