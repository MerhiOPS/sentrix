package main

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// skipDirs are never scanned: tooling output, dependencies, and test
// fixtures that violate the rules on purpose.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"testdata":     true,
	"dist":         true,
}

// walkFiles calls fn for every regular file under root whose extension is in
// exts. rel is slash-separated and relative to root.
func walkFiles(root string, exts []string, fn func(abs, rel string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !hasExt(path, exts) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return fn(path, filepath.ToSlash(rel))
	})
}

func hasExt(path string, exts []string) bool {
	for _, e := range exts {
		if strings.HasSuffix(path, e) {
			return true
		}
	}
	return false
}
