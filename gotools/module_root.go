package gotools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

const TheoryOfNonGoFiles = `
Non-Go project files — embed files, other package files, and markdown —
are never emitted at full content in the initial context; they are
present by name only. Package-anchored non-Go files appear in the focus
documentation block's file-names section, so the model knows they exist
and reads or writes them on demand with ingest blocks and change blocks.
This extends the doc-first context strategy (TheoryOfContextStrategy)
to non-Go content: the name list is the index, the ingest block is the
fetch.

A module root may carry no Go package (no .go files at the root), so its
markdown files never become package files and would be invisible to the
package file lists. GetModuleRootFiles closes the gap: it enumerates the
module root when it equals the load directory plus the root of every
workspace module in workspace mode, skipping directories that are
themselves package directories (their markdown is already a package
file listed in the package's file-names section), and
PartsProvider.Parts emits one listing part per module root naming its
markdown files.

The -match filter and "!" exclusion patterns apply to listed names
exactly as to collected files, so a listed name is always one the
pipeline would include. Under -all-src, focus packages are pinned at
full source, so non-Go focus files are full content there — an explicit
opt-in. See TheoryOfVisibilityAllocation.
`

// ModuleRootFiles is the markdown listing of one module root directory.
// See TheoryOfNonGoFiles.
type ModuleRootFiles struct {
	// Dir is the module root directory.
	Dir string
	// Files holds the paths of the markdown files at Dir.
	Files []string
}

// GetModuleRootFiles returns the module-root markdown listings. It is a
// scope-cached provider resolved once. See TheoryOfNonGoFiles.
type GetModuleRootFiles func() ([]ModuleRootFiles, error)

// ModuleRootFiles provider: enumerates the markdown files of module-root
// directories that carry no Go package, so their top-level documentation
// stays discoverable without being emitted at full content. See
// TheoryOfNonGoFiles.
func (Module) ModuleRootFiles(
	getRootPackages GetRootPackages,
	loadDir LoadDir,
	workspace Workspace,
	hidden HiddenPatterns,
) GetModuleRootFiles {
	return sync.OnceValues(func() (list []ModuleRootFiles, err error) {
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		isHidden := newHiddenPackageMatcher(hidden)

		// Package directories collect their own markdown as package
		// files, so a directory that is itself a (non-hidden) package
		// directory must not also produce a listing: the same file
		// would appear twice.
		rootPkgDirs := make(map[string]bool)
		for _, pkg := range rootPkgs {
			if isHidden != nil && isHidden(pkg.PkgPath) {
				continue
			}
			for _, file := range pkg.GoFiles {
				rootPkgDirs[filepath.Dir(file)] = true
				break
			}
		}

		// The load directory's module root is listed only when it equals
		// the load directory: when loading from a subdirectory, the
		// module root may contain files outside the writable
		// directories, and pulling them in would surface content the
		// focus file writable check exists to guard. The first
		// module-bearing non-hidden root package decides.
		var dirs []string
		loadDirPath := filepath.Clean(string(loadDir))
		for _, pkg := range rootPkgs {
			if isHidden != nil && isHidden(pkg.PkgPath) {
				continue
			}
			if pkg.Module != nil && pkg.Module.Dir != "" {
				if rootDir := filepath.Clean(pkg.Module.Dir); rootDir == loadDirPath {
					dirs = append(dirs, rootDir)
				}
				break
			}
		}
		// In workspace mode, the root of every workspace module is
		// listed so top-level documentation in each module is
		// discoverable. See TheoryOfWorkspace.
		if workspace != "" {
			for _, moduleDir := range workspaceModules(string(workspace)) {
				dirs = append(dirs, filepath.Clean(moduleDir))
			}
		}
		slices.Sort(dirs)
		dirs = slices.Compact(dirs)

		for _, dir := range dirs {
			if rootPkgDirs[dir] {
				continue
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			// Sort entries by name for deterministic ordering; a
			// filesystem-dependent order would change the listing
			// between runs and invalidate the LLM prefix cache.
			slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
				return strings.Compare(a.Name(), b.Name())
			})
			var files []string
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				lowerName := strings.ToLower(name)
				if strings.HasSuffix(lowerName, ".md") && !strings.HasPrefix(lowerName, "_") {
					files = append(files, filepath.Join(dir, name))
				}
			}
			if len(files) == 0 {
				continue
			}
			list = append(list, ModuleRootFiles{Dir: dir, Files: files})
		}
		return list, nil
	})
}
