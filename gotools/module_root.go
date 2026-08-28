package gotools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/reusee/tai/anytexts"
)

const TheoryOfNonGoFiles = `
Non-Go project files — embed files, other package files, and structural
text — are never emitted at full content in the initial context; they are
present by name only. Package-anchored non-Go files appear in the focus
documentation block's file-names section, so the model knows they exist
and reads or writes them on demand with ingest blocks and change blocks.
This extends the doc-first context strategy (TheoryOfContextStrategy)
to non-Go content: the name list is the index, the ingest block is the
fetch.

A module root may carry no Go package (no .go files at the root), so its
structural text files never become package files and would be invisible
to the package file lists. GetModuleRootFiles closes the gap: it
enumerates the module root when it equals the load directory plus the
root of every workspace module in workspace mode, skipping directories
that are themselves package directories (their non-Go files are already
package files listed in the package's file-names section), and
PartsProvider.Parts emits one listing part per module root naming its
structural text files — every file whose path a gotreesitter grammar
recognizes, so formats the grammar library adds later are listed
automatically. Each listed file carries a parsed skeleton when one is
extractable (anytexts.Skeleton), so the model sees the document's
structure before deciding to fetch it. A skeleton is a summary:
modifying or fully understanding the file still requires
fetching the original with an ingest block. The listing's header
states the summary form once; per-file consumption rules — treat the
skeleton as an index, fetch the original before modifying — live in the
system prompt (pipeline.SkeletonFilesSystemPrompt), and the listing body
carries no repeated hint text.

The -match filter and "!" exclusion patterns apply to listed names
exactly as to collected files, so a listed name is always one the
pipeline would include. Under -all-src, focus packages are pinned at
full source, so non-Go focus files are full content there — an explicit
opt-in. See TheoryOfVisibilityAllocation.
`

// ModuleRootFiles is the structural text file listing of one module root
// directory. See TheoryOfNonGoFiles.
type ModuleRootFiles struct {
	// Dir is the module root directory.
	Dir string
	// Files holds the paths of the structural text files at Dir: every
	// file whose path a gotreesitter grammar recognizes. See
	// anytexts.SkeletonSupported.
	Files []string
	// Skeletons maps a file path from Files to its parsed structural
	// skeleton (e.g., a markdown heading outline or a code file's
	// definition outline). Files without an extractable skeleton are
	// absent. See anytexts.TheoryOfContextSkeleton.
	Skeletons map[string]string
}

// GetModuleRootFiles returns the module-root structural text listings.
// It is a scope-cached provider resolved once. See TheoryOfNonGoFiles.
type GetModuleRootFiles func() ([]ModuleRootFiles, error)

// ModuleRootFiles provider: enumerates the structural text files of
// module-root directories that carry no Go package, so their top-level
// documentation stays discoverable without being emitted at full
// content. A file is structural when gotreesitter's grammar registry
// recognizes its path, so every grammar the library ships — and any
// grammar added to it later — is listed automatically. Each listed file
// carries its parsed skeleton when one is extractable; files without an
// extractable skeleton stay name-only. See TheoryOfNonGoFiles and
// anytexts.TheoryOfContextSkeleton.
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

		// Package directories collect their own non-Go files as package
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
				// Hidden files are skipped, matching the traversal
				// semantics of anytexts.PartsProvider; _-prefixed files
				// stay skipped as before.
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					continue
				}
				path := filepath.Join(dir, name)
				if anytexts.SkeletonSupported(path) {
					files = append(files, path)
				}
			}
			if len(files) == 0 {
				continue
			}
			listing := ModuleRootFiles{Dir: dir, Files: files}
			// Extract a parsed skeleton for every listed file. Extraction
			// is best-effort: a read error or an unsupported structure
			// leaves the file name-only, and a read error is not fatal
			// because the file's content is not being served here. See
			// anytexts.TheoryOfContextSkeleton.
			skeletons := make(map[string]string)
			for _, path := range files {
				content, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				if skeleton, ok := anytexts.Skeleton(path, content); ok {
					skeletons[path] = skeleton
				}
			}
			if len(skeletons) > 0 {
				listing.Skeletons = skeletons
			}
			list = append(list, listing)
		}
		return list, nil
	})
}
