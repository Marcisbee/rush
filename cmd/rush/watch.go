package main

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type watchedFileState struct {
	exists   bool
	size     int64
	modified int64
}

func mergeWatchFiles(cwd string, suites, dependencies []string) []string {
	seen := make(map[string]bool, len(suites)+len(dependencies))
	files := make([]string, 0, len(suites)+len(dependencies))
	add := func(path string) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	for _, path := range suites {
		add(path)
	}
	for _, path := range dependencies {
		add(path)
	}
	sort.Strings(files)
	return files
}

func waitForFileChange(ctx context.Context, files []string) (string, error) {
	states := make(map[string]watchedFileState, len(files))
	for _, path := range files {
		states[path] = statWatchedFile(path)
	}
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			for _, path := range files {
				if current := statWatchedFile(path); current != states[path] {
					return path, nil
				}
			}
		}
	}
}

func statWatchedFile(path string) watchedFileState {
	info, err := os.Stat(path)
	if err != nil {
		return watchedFileState{}
	}
	return watchedFileState{exists: true, size: info.Size(), modified: info.ModTime().UnixNano()}
}

func displayPath(cwd, path string) string {
	relative, err := filepath.Rel(cwd, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.ToSlash(relative)
}
