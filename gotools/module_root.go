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

A directory that anchors no Go package holds structural text that no
package file list would ever name: the module root without .go files,
a docs directory, a fixtures directory. GetModuleFiles closes the gap:
from the load directory's module root (when it equals the load
directory) and from every workspace module root, it walks the module
tree and yields one listing per non-package directory. A directory is a
package directory when any collected project file lives in it; the
anchor is read from the same load the context is built from, so root,
context, and dependency packages are covered alike. Package directories
produce no listing of their own files but are still traversed, so
non-package subdirectories beneath them are listed.

The walk follows anytext traversal semantics: hidden and
underscore-prefixed names are skipped, and non-regular entries are
skipped, so directory symlinks are neither followed nor listed and the
walk stays inside the module tree, free of cycles. A nested go.mod
marks a different module: its directory is neither listed nor
traversed; workspace mode enumerates those module roots separately.
vendor directories are skipped: they hold machine-managed dependencies,
never project documentation. PartsProvider.Parts emits one listing part
per non-package directory naming its structural text files — every file
whose path a gotreesitter grammar recognizes, so formats the grammar
library adds later are listed automatically. Each listed file carries a
parsed skeleton when one is extractable (anytexts.Skeleton), so the
model sees the document's structure before deciding to fetch it. A
skeleton is a summary: modifying or fully understanding the file still
requires fetching the original with an ingest block. The listing's
header states the summary form once; per-file consumption rules — treat
the skeleton as an index, fetch the original before modifying — live in
the system prompt (pipeline.SkeletonFilesSystemPrompt), and the listing
body carries no repeated hint text.

The -match filter and "!" exclusion patterns apply to listed names
exactly as to collected files, so a listed name is always one the
pipeline would include. Like extras, listing parts are truncated from
the end when the token budget is exhausted, so listings included in
smaller-budget requests keep their positions in larger-budget requests.
Under -all-src, focus packages are pinned at full source, so non-Go
focus files are full content there — an explicit opt-in. See
TheoryOfVisibilityAllocation.
`

// ModuleFiles is the structural text file listing of one non-package
// directory under a module. See TheoryOfNonGoFiles.
type ModuleFiles struct {
	// Dir is the listed directory.
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

// GetModuleFiles returns the structural text listings of every
// non-package directory under the module. It is a scope-cached provider
// resolved once. See TheoryOfNonGoFiles.
type GetModuleFiles func() ([]ModuleFiles, error)

// ModuleFiles provider: enumerates the structural text files of every
// non-package directory under the module, so documentation in any
// directory stays discoverable without being emitted at full content.
// A file is structural when gotreesitter's grammar registry recognizes
// its path, so every grammar the library ships — and any grammar added
// to it later — is listed automatically. Each listed file carries its
// parsed skeleton when one is extractable; files without an extractable
// skeleton stay name-only. See TheoryOfNonGoFiles and
// anytexts.TheoryOfContextSkeleton.
func (Module) ModuleFiles(
	getRootPackages GetRootPackages,
	getFiles GetFiles,
	loadDir LoadDir,
	workspace Workspace,
	hidden HiddenPatterns,
) GetModuleFiles {
	return sync.OnceValues(func() (list []ModuleFiles, err error) {
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		files, err := getFiles()
		if err != nil {
			return nil, err
		}
		isHidden := newHiddenPackageMatcher(hidden)

		// A directory that holds any collected project file is a package
		// directory: its files are package-anchored and are never listed.
		// Every collected file — Go, embed, and other package files —
		// anchors its directory, so root, context, and dependency
		// packages are covered alike. See TheoryOfNonGoFiles.
		packageDirs := make(map[string]bool)
		for _, file := range files {
			if file.Path == file.LogicalPkgPath {
				continue
			}
			packageDirs[filepath.Dir(file.Path)] = true
		}

		// The load directory's module root is enumerated only when it
		// equals the load directory: when loading from a subdirectory,
		// the module root may contain files outside the writable
		// directories, and pulling them in would surface content the
		// focus file writable check exists to guard. The first
		// module-bearing non-hidden root package decides.
		var roots []string
		loadDirPath := filepath.Clean(string(loadDir))
		for _, pkg := range rootPkgs {
			if isHidden != nil && isHidden(pkg.PkgPath) {
				continue
			}
			if pkg.Module != nil && pkg.Module.Dir != "" {
				if rootDir := filepath.Clean(pkg.Module.Dir); rootDir == loadDirPath {
					roots = append(roots, rootDir)
				}
				break
			}
		}
		// In workspace mode, the root of every workspace module is
		// enumerated so top-level documentation in each module is
		// discoverable. See TheoryOfWorkspace.
		if workspace != "" {
			for _, moduleDir := range workspaceModules(string(workspace)) {
				roots = append(roots, filepath.Clean(moduleDir))
			}
		}
		slices.Sort(roots)
		roots = slices.Compact(roots)

		// Breadth-first walk from every root, entries in ReadDir's sorted
		// order, so the listing set and its order are deterministic
		// across runs and preserve the LLM prefix cache.
		rootSet := make(map[string]bool, len(roots))
		for _, dir := range roots {
			rootSet[dir] = true
		}
		queue := slices.Clone(roots)
		visited := make(map[string]bool)
		for len(queue) > 0 {
			dir := queue[0]
			queue = queue[1:]
			if visited[dir] {
				continue
			}
			visited[dir] = true

			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			isPkg := packageDirs[dir]
			var structural []string
			var subdirs []string
			nestedModule := false
			for _, entry := range entries {
				name := entry.Name()
				// Hidden and underscore-prefixed names are skipped,
				// matching the traversal semantics of
				// anytexts.PartsProvider.
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					continue
				}
				if entry.IsDir() {
					// vendor holds machine-managed dependencies, never
					// project documentation.
					if name == "vendor" {
						continue
					}
					subdirs = append(subdirs, filepath.Join(dir, name))
					continue
				}
				// Non-regular entries — symlinks, pipes, sockets, devices —
				// are skipped: directory symlinks are neither followed nor
				// listed, so the walk stays inside the module tree, free of
				// symlink cycles. See TheoryOfNonGoFiles.
				if entry.Type()&(os.ModeSymlink|os.ModeNamedPipe|os.ModeSocket|os.ModeDevice|os.ModeIrregular) != 0 {
					continue
				}
				if name == "go.mod" {
					nestedModule = true
					continue
				}
				path := filepath.Join(dir, name)
				if !isPkg && anytexts.SkeletonSupported(path) {
					structural = append(structural, path)
				}
			}
			// A nested go.mod marks a different module: its directory is
			// neither listed nor traversed. Workspace mode enumerates
			// those module roots separately. See TheoryOfWorkspace.
			if nestedModule && !rootSet[dir] {
				continue
			}
			queue = append(queue, subdirs...)

			if isPkg || len(structural) == 0 {
				continue
			}
			listing := ModuleFiles{Dir: dir, Files: structural}
			// Extract a parsed skeleton for every listed file. Extraction
			// is best-effort: a read error or an unsupported structure
			// leaves the file name-only, and a read error is not fatal
			// because the file's content is not being served here. See
			// anytexts.TheoryOfContextSkeleton.
			skeletons := make(map[string]string)
			for _, path := range structural {
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
		slices.SortFunc(list, func(a, b ModuleFiles) int {
			return strings.Compare(a.Dir, b.Dir)
		})
		return list, nil
	})
}
