package main

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// codemapFile is the navigation map every package must appear in.
const codemapFile = "docs/CODEMAP.md"

// repoPath is a backticked path the map names, checked for existence.
var repoPath = regexp.MustCompile("`((?:internal|cmd|web|scripts|docs)/[^`\\s]*)`")

// checkCodemap keeps docs/CODEMAP.md in step with the tree: every directory
// holding Go files is listed (as `dir/`), and every repository path the map
// names exists. A map that drifts is worse than none.
func checkCodemap(root string) ([]Violation, error) {
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(codemapFile)))
	if errors.Is(err, os.ErrNotExist) {
		return []Violation{{Check: "codemap", Path: codemapFile, Msg: "missing: the code map lists every package"}}, nil
	}
	if err != nil {
		return nil, err
	}
	doc := string(raw)
	seen := map[string]bool{}
	var out []Violation
	err = walkFiles(root, []string{".go"}, func(_, rel string) error {
		dir := path.Dir(rel)
		if dir == "." || seen[dir] {
			return nil
		}
		seen[dir] = true
		if !strings.Contains(doc, "`"+dir+"/`") {
			out = append(out, Violation{Check: "codemap", Path: dir, Msg: "package missing from " + codemapFile})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, m := range repoPath.FindAllStringSubmatch(doc, -1) {
		rel := filepath.FromSlash(m[1])
		if !filepath.IsLocal(rel) {
			out = append(out, Violation{Check: "codemap", Path: codemapFile, Msg: "names " + m[1] + ", outside the repository"})
			continue
		}
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil { //nolint:gosec // a local, repo-relative path (checked above)
			out = append(out, Violation{Check: "codemap", Path: codemapFile, Msg: "names " + m[1] + ", which does not exist"})
		}
	}
	return out, nil
}
