package pathutil

import (
	"os"
	"path/filepath"
)

// TheoryOfModuleRoot explains why the go module root walk lives here as a
// single shared primitive.
const TheoryOfModuleRoot = `
The nearest go.mod at or above a directory marks its Go module boundary.
taiconfigs (module root as a config-file root), gotools (import-path to
directory mapping under the working directory's module), and cmd/tai
(default-command detection) all need that boundary and must agree on it,
so the upward walk is implemented once here and every consumer derives
its own semantics — existence, path, or a readable module path — from
the single result.
`

// FindGoModuleRoot returns the absolute path of the nearest directory at
// or above dir containing a go.mod file, and whether one exists. A go.mod
// that is itself a directory does not count; the walk skips it and
// continues upward, stopping at the filesystem root. See
// TheoryOfModuleRoot.
func FindGoModuleRoot(dir string) (root string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		dir = filepath.Clean(dir)
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
