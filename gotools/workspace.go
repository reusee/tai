package gotools

import (
	"os"
	"path/filepath"
	"strings"
)

const TheoryOfWorkspace = `
Go workspace (go.work) support loads every module in the workspace as part of
the project. When the load directory is the workspace root or the root of a
module listed in a go.work file, packages are loaded from the workspace root
so that all workspace packages become root (focus) packages. The go command
rejects the default pattern "./..." from a non-module workspace root, so in
workspace mode the default "./..." is replaced with one "./<module>/..."
pattern per workspace module. The root of each workspace module is scanned
for top-level documentation (README.md) just like the load directory's
module root.

Workspace mode activates only when the load directory is the workspace root
or the root of a module listed in go.work whose root lies strictly below the
workspace root. Running from a subdirectory of a module keeps module-scoped
loading: a go.work file above the current module (for example, a developer
workspace that lists the module being edited) does not change the scope of
packages loaded from deep subdirectories. Running from a subdirectory of the
workspace root's own module (a directory that carries both go.mod and
go.work) also stays on the non-workspace path so that "./..." remains
relative to the load directory. A go.work file found higher up that does not
include the load directory's module does not activate workspace mode,
mirroring the go command's own scoping rules. GOWORK=off disables workspace
mode; a GOWORK environment variable pointing to a go.work file activates it
for that workspace. Patterns given via -pkg or -ctx are resolved relative to
the workspace root in workspace mode, matching the behavior of running the
go command from the workspace root.

Focus files in workspace modules outside the writable directories are marked
read-only, consistent with the existing focus file safety policy. Running
the tool from the workspace root keeps modules located under it within the
writable directories.
`

// Workspace is the root directory of the Go workspace that the load
// directory belongs to, or empty when the load directory is not part of
// a workspace.
type Workspace string

func (Module) Workspace(loadDir LoadDir) Workspace {
	// GOWORK=off disables workspace mode. A GOWORK value pointing to a
	// go.work file makes that workspace authoritative.
	if gowork, ok := os.LookupEnv("GOWORK"); ok {
		if gowork == "off" {
			return ""
		}
		if gowork != "" {
			if !filepath.IsAbs(gowork) {
				if cwd, err := os.Getwd(); err == nil {
					gowork = filepath.Join(cwd, gowork)
				}
			}
			if _, err := os.Stat(gowork); err == nil {
				return verifyWorkspace(filepath.Dir(gowork), string(loadDir))
			}
		}
	}

	// Walk up from the load directory to find go.work.
	dir, err := filepath.Abs(string(loadDir))
	if err != nil {
		dir = filepath.Clean(string(loadDir))
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return verifyWorkspace(dir, string(loadDir))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// verifyWorkspace activates workspace mode only when the load directory is
// the workspace root or the root of a module listed in the workspace's
// go.work. See TheoryOfWorkspace.
func verifyWorkspace(root string, loadDir string) Workspace {
	root = filepath.Clean(root)
	loadDirAbs, err := filepath.Abs(loadDir)
	if err != nil {
		loadDirAbs = filepath.Clean(loadDir)
	}
	loadDirAbs = filepath.Clean(loadDirAbs)
	if loadDirAbs == root {
		return Workspace(root)
	}

	// Find the innermost go.mod of the load directory, bounded by the
	// workspace root.
	moduleRoot := ""
	for dir := loadDirAbs; ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			moduleRoot = dir
			break
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if moduleRoot == "" {
		return ""
	}
	// A module whose root coincides with the workspace root stays on the
	// non-workspace path. See TheoryOfWorkspace.
	if moduleRoot == root {
		return ""
	}
	// Only the module root itself activates workspace mode; subdirectories
	// keep module-scoped loading. See TheoryOfWorkspace.
	if filepath.Clean(moduleRoot) != loadDirAbs {
		return ""
	}
	for _, moduleDir := range workspaceModules(root) {
		if filepath.Clean(moduleDir) == moduleRoot {
			return Workspace(root)
		}
	}
	return ""
}

// workspaceModules returns the absolute paths of the module directories
// listed in the use directives of the go.work file at the workspace root.
func workspaceModules(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		return nil
	}
	var modules []string
	inUseBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inUseBlock {
			if trimmed == "use (" || strings.HasPrefix(trimmed, "use (") {
				inUseBlock = true
				continue
			}
			if strings.HasPrefix(trimmed, "use ") {
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "use"))
				if rest != "" && !strings.HasPrefix(rest, "//") {
					modules = append(modules, filepath.Join(root, filepath.FromSlash(unquoteGoWorkPath(strings.Fields(rest)[0]))))
				}
			}
			continue
		}
		if trimmed == ")" {
			inUseBlock = false
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		modules = append(modules, filepath.Join(root, filepath.FromSlash(unquoteGoWorkPath(strings.Fields(trimmed)[0]))))
	}
	return modules
}

// unquoteGoWorkPath removes surrounding quotes from a go.work use path.
// go.work accepts quoted paths; without unquoting, filepath.Join would
// treat the quotes as part of the directory name.
func unquoteGoWorkPath(s string) string {
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}
