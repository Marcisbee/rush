// Package watch maps changed modules to the smallest affected suite set.
package watch

import (
	"path/filepath"
	"sort"
)

type Graph struct {
	suites  map[string]bool
	forward map[string]map[string]bool
	reverse map[string]map[string]bool
}

func New() *Graph {
	return &Graph{suites: map[string]bool{}, forward: map[string]map[string]bool{}, reverse: map[string]map[string]bool{}}
}

// Add extends the direct dependencies known for one module.
func (g *Graph) Add(module string, dependencies ...string) {
	module = clean(module)
	known := make([]string, 0, len(g.forward[module])+len(dependencies))
	for dependency := range g.forward[module] {
		known = append(known, dependency)
	}
	known = append(known, dependencies...)
	g.Update(module, known...)
}

// Update replaces a module's imports after an incremental esbuild rebuild,
// removing stale reverse edges before recording the new dependency set.
func (g *Graph) Update(module string, dependencies ...string) {
	module = clean(module)
	for dependency := range g.forward[module] {
		delete(g.reverse[dependency], module)
		if len(g.reverse[dependency]) == 0 {
			delete(g.reverse, dependency)
		}
	}
	g.forward[module] = map[string]bool{}
	for _, dependency := range dependencies {
		dependency = clean(dependency)
		g.forward[module][dependency] = true
		if g.reverse[dependency] == nil {
			g.reverse[dependency] = map[string]bool{}
		}
		g.reverse[dependency][module] = true
	}
}

func (g *Graph) Suite(path string) {
	path = clean(path)
	g.suites[path] = true
	if g.forward[path] == nil {
		g.forward[path] = map[string]bool{}
	}
}

// Affected returns suites that directly or transitively import a changed file.
func (g *Graph) Affected(changed ...string) []string {
	seen := map[string]bool{}
	queue := make([]string, 0, len(changed))
	for _, path := range changed {
		queue = append(queue, clean(path))
	}
	var suites []string
	for len(queue) > 0 {
		module := queue[0]
		queue = queue[1:]
		if seen[module] {
			continue
		}
		seen[module] = true
		if g.suites[module] {
			suites = append(suites, module)
		}
		for importer := range g.reverse[module] {
			queue = append(queue, importer)
		}
	}
	sort.Strings(suites)
	return suites
}

func (g *Graph) All() []string {
	suites := make([]string, 0, len(g.suites))
	for suite := range g.suites {
		suites = append(suites, suite)
	}
	sort.Strings(suites)
	return suites
}

func clean(path string) string { return filepath.ToSlash(filepath.Clean(path)) }
