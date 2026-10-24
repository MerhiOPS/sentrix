package main

import "strings"

// layer is a named region of the module with the module-internal imports it
// may use and whether it may import third-party packages.
type layer struct {
	name       string
	prefix     string   // module-relative directory, slash-separated
	mayImport  []string // module-relative prefixes this layer may import
	thirdParty bool
}

// layers is ordered most-specific first: the first matching prefix wins.
// It encodes CLAUDE.md §3 and PLAN.md §3 rule 3.
var layers = []layer{
	{name: "domain", prefix: "internal/domain",
		mayImport: []string{"internal/domain"}},
	{name: "wire", prefix: "internal/wire",
		mayImport: []string{"internal/wire"}},
	{name: "app", prefix: "internal/app",
		mayImport: []string{"internal/domain", "internal/app"}},
	{name: "ingesthttp", prefix: "internal/infra/ingesthttp", thirdParty: true,
		mayImport: []string{"internal/domain", "internal/app", "internal/infra", "internal/wire"}},
	{name: "infra", prefix: "internal/infra", thirdParty: true,
		mayImport: []string{"internal/domain", "internal/app", "internal/infra"}},
	{name: "agent", prefix: "internal/agent", thirdParty: true,
		mayImport: []string{"internal/domain", "internal/agent", "internal/wire",
			"internal/infra/collector", "internal/infra/logtail", "internal/infra/tlscert"}},
}

// layerFor returns the layer owning a module-relative directory, or nil for
// code outside the layered tree (cmd/, scripts/), which may import anything.
func layerFor(dir string) *layer {
	for i := range layers {
		if underPrefix(dir, layers[i].prefix) {
			return &layers[i]
		}
	}
	return nil
}

func (l *layer) allows(moduleRel string) bool {
	for _, p := range l.mayImport {
		if underPrefix(moduleRel, p) {
			return true
		}
	}
	return false
}

func underPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

// isStdlib reports whether an import path belongs to the standard library:
// stdlib paths have no dot in their first element.
func isStdlib(importPath string) bool {
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}
