package gocodes

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/logs"
	"golang.org/x/tools/go/packages"
)

const TheoryOfFileOrdering = `
Files are sorted in three tiers to maximize LLM prefix cache reuse. The outermost
tier separates by module: non-root-module files (dependencies, stdlib) appear first,
forming the stable prefix that changes least frequently across requests. The middle
tier separates root-module files by package: context files (non-root packages) precede
focus files (root package), so that editing a focus file does not shift the position
of any context file. The inner tiers (go vs non-go, distance, package depth, package
path) further organize files within each group for deterministic ordering.

When only focus files change, all preceding context and dependency files remain
identical, allowing LLM prefix caching to reuse cached key-value states for unchanged
content.

Within each priority group, files are ordered by their path as the primary key for
a fully deterministic order independent of modification times, maximizing cache reuse.
Modification time is a final tiebreaker.
`

const TheoryOfFileLoadingPerformance = `
Go file reading and parsing in Files is parallelized to reduce wall-clock
latency on multi-core machines: each file in the dependency graph is read
and parsed independently, with a bounded worker pool so file descriptors
are not exhausted on large trees. token.FileSet methods are synchronized
(Go 1.9+), so a shared fset can be passed to parser.ParseFile from multiple
goroutines safely. Results are collected in an indexed slice that preserves
the deterministic input order.

The raw content of every Go file is cached in File.Content at load time.
The simplification pipeline renders files from this cached content instead
of re-reading from disk, so each file is read exactly once per run.
`

type File struct {
	Path                    string
	IsGoFile                bool
	IsTestFile              bool
	Content                 []byte
	TokenFile               *token.File
	AstFile                 *ast.File
	Package                 *packages.Package
	PackageIsRoot           bool
	PackageDistanceFromRoot int
	PackagePathDepth        int
	Module                  *packages.Module
	ModuleIsRoot            bool
	ModuleIsNil             bool
	IsEmbed                 bool
	DoNotSimplify           bool
	ReadOnly                bool
	LogicalPkgPath          string

	Confirmed *Transformed
	ModTime   time.Time
}

type Transformed struct {
	What      string
	Content   []byte
	NumTokens int
}

type GetFiles func() ([]*File, error)

func (Module) Files(
	getFileSet GetFileSet,
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
	logger logs.Logger,
	debug Debug,
	loadDir LoadDir,
	workspace Workspace,
) GetFiles {
	return sync.OnceValues(func() (files []*File, err error) {

		fset, err := getFileSet()
		if err != nil {
			return nil, err
		}

		// packages
		rootPkgs, err := getRootPackages()
		if err != nil {
			return nil, err
		}
		contextPkgs, err := getContextPackages()
		if err != nil {
			return nil, err
		}

		// Collect all packages from the dependency graph by walking imports.
		// No distance limit; the visibility system in visibility.go determines
		// which packages are visible based on the 32K context budget.
		// See TheoryOfLightweightPackageLoading in packages.go.
		allPkgsSet := make(map[*packages.Package]bool)
		var walkImports func(pkg *packages.Package)
		walkImports = func(pkg *packages.Package) {
			for _, imp := range pkg.Imports {
				if imp == nil || allPkgsSet[imp] {
					continue
				}
				allPkgsSet[imp] = true
				walkImports(imp)
			}
		}
		for _, pkg := range rootPkgs {
			allPkgsSet[pkg] = true
			walkImports(pkg)
		}
		for _, pkg := range contextPkgs {
			if !allPkgsSet[pkg] {
				allPkgsSet[pkg] = true
				walkImports(pkg)
			}
		}

		// Convert to sorted slice for deterministic ordering.
		allPkgs := make([]*packages.Package, 0, len(allPkgsSet))
		for pkg := range allPkgsSet {
			allPkgs = append(allPkgs, pkg)
		}
		slices.SortStableFunc(allPkgs, func(a, b *packages.Package) int {
			return cmp.Compare(a.PkgPath, b.PkgPath)
		})

		// rootPkgSet provides O(1) root package membership checks.
		rootPkgSet := make(map[*packages.Package]bool, len(rootPkgs))
		for _, pkg := range rootPkgs {
			rootPkgSet[pkg] = true
		}

		// root modules
		// Only the modules of root and context packages are root modules.
		// Dependency packages discovered via import walking may belong to
		// external modules. Marking an external module as root would classify
		// its files as root-module files, causing them to bypass the
		// non-root-module simplification transforms (comment stripping,
		// function body deletion) and the deletion transforms that enforce
		// the context token budget. See TheoryOfFileOrdering.
		rootModulePaths := make(map[string]bool)
		for _, pkg := range rootPkgs {
			if pkg.Module != nil {
				rootModulePaths[pkg.Module.Path] = true
			}
		}
		for _, pkg := range contextPkgs {
			if pkg.Module != nil {
				rootModulePaths[pkg.Module.Path] = true
			}
		}
		if debug {
			for _, pkg := range allPkgs {
				logger.Info("loaded package", "path", pkg.PkgPath)
			}
		}

		// Discover Go files from pkg.goFiles and parse individually.
		// All packages in the dependency graph are included; the visibility
		// system determines which are visible based on the 32K context budget.
		// See TheoryOfLightweightPackageLoading in packages.go.
		seenFilePaths := make(map[string]bool)
		type goFileEntry struct {
			path string
			pkg  *packages.Package
		}
		var allGoFiles []goFileEntry
		for _, pkg := range allPkgs {
			for _, path := range pkg.GoFiles {
				if !strings.HasSuffix(path, ".go") {
					continue
				}
				if seenFilePaths[path] {
					continue
				}
				seenFilePaths[path] = true
				allGoFiles = append(allGoFiles, goFileEntry{path: path, pkg: pkg})
			}
		}
		slices.SortStableFunc(allGoFiles, func(a, b goFileEntry) int {
			return cmp.Compare(a.path, b.path)
		})

		// Read and parse Go files in parallel to reduce wall-clock latency on
		// multi-core machines. Results are collected in an indexed slice that
		// preserves the deterministic input order; the final sort establishes
		// the canonical output order regardless of completion order.
		// token.FileSet methods are synchronized (Go 1.9+), so a shared fset
		// can be passed to parser.ParseFile from multiple goroutines.
		// Raw content is cached in File.Content so the simplification
		// pipeline renders from memory instead of re-reading from disk.
		// See TheoryOfFileLoadingPerformance.
		goFileResults := make([]*File, len(allGoFiles))
		var goWg sync.WaitGroup
		goSem := make(chan struct{}, 16) // bounded concurrency for I/O-bound work

		for i, entry := range allGoFiles {
			goWg.Add(1)
			goSem <- struct{}{}
			go func(i int, entry goFileEntry) {
				defer goWg.Done()
				defer func() { <-goSem }()

				pkg := entry.pkg

				path := entry.path
				f := &File{
					Path:     path,
					IsGoFile: true,
				}
				if info, err := os.Stat(path); err == nil {
					f.ModTime = info.ModTime()
				}

				// Parse the file individually.
				src, err := os.ReadFile(path)
				if err != nil {
					logger.Warn("cannot read go file", "path", path, "error", err)
					return
				}
				astFile, err := parser.ParseFile(fset, path, src, parser.ParseComments)
				if err != nil {
					logger.Warn("cannot parse go file", "path", path, "error", err)
					return
				}
				f.TokenFile = fset.File(astFile.Pos())
				f.AstFile = astFile

				f.Package = pkg
				f.PackageIsRoot = rootPkgSet[pkg]
				f.PackageDistanceFromRoot = 0 // overwritten by simplify.go
				f.PackagePathDepth = len(strings.Split(pkg.PkgPath, "/"))
				f.Module = pkg.Module
				f.ModuleIsRoot = pkg.Module != nil && rootModulePaths[pkg.Module.Path]
				f.ModuleIsNil = pkg.Module == nil

				// Cache the raw content so renderFileAtLevel does not
				// re-read the file from disk. See
				// TheoryOfFileLoadingPerformance.
				f.Content = src

				goFileResults[i] = f
			}(i, entry)
		}
		goWg.Wait()

		for _, f := range goFileResults {
			if f != nil {
				files = append(files, f)
			}
		}

		// collect non-Go files
		nonGoFilePaths := make(map[string]*packages.Package)
		embedFilePaths := make(map[string]bool)
		for _, pkg := range allPkgs {
			for _, path := range pkg.EmbedFiles {
				if seenFilePaths[path] {
					continue
				}
				embedFilePaths[path] = true
				if _, ok := nonGoFilePaths[path]; !ok {
					nonGoFilePaths[path] = pkg
				}
			}
			for _, path := range pkg.OtherFiles {
				if seenFilePaths[path] {
					continue
				}
				if _, ok := nonGoFilePaths[path]; !ok {
					nonGoFilePaths[path] = pkg
				}
			}
		}

		// root packages directories
		rootPkgDirs := make(map[string]*packages.Package)
		for _, pkg := range rootPkgs {
			for _, file := range pkg.GoFiles {
				rootPkgDirs[filepath.Dir(file)] = pkg
				break
			}
		}
		// Also include the module root directory for .md file scanning.
		// When the module root has no direct .go files (e.g., all Go code
		// lives in subdirectories), the root directory does not appear in
		// rootPkgDirs and top-level documentation like README.md would be
		// missed. Associate the root directory with an existing root
		// package so .md files discovered there are treated as root
		// package files (PackageIsRoot=true) and survive simplification.
		// Only add the module root when it matches the LoadDir. When
		// LoadDir is a subdirectory of the module root, the module root
		// may contain files outside the writable directories (e.g., when
		// running tests from a package subdirectory), and pulling them in
		// would cause the focus file writable check to reject them at
		// collection time.
		loadDirPath := filepath.Clean(string(loadDir))
		for _, pkg := range rootPkgs {
			if pkg.Module != nil && pkg.Module.Dir != "" {
				rootDir := filepath.Clean(pkg.Module.Dir)
				if rootDir == loadDirPath {
					if _, ok := rootPkgDirs[rootDir]; !ok {
						rootPkgDirs[rootDir] = pkg
					}
				}
				break
			}
		}
		// In workspace mode, add the root of every workspace module so
		// that top-level documentation (README.md) in each module is
		// discovered. See TheoryOfWorkspace.
		if workspace != "" {
			for _, moduleDir := range workspaceModules(string(workspace)) {
				moduleDir = filepath.Clean(moduleDir)
				if _, ok := rootPkgDirs[moduleDir]; ok {
					continue
				}
				for _, pkg := range rootPkgs {
					if pkg.Module != nil && filepath.Clean(pkg.Module.Dir) == moduleDir {
						rootPkgDirs[moduleDir] = pkg
						break
					}
				}
			}
		}
		// include .md files in root package directories
		// Sort directories for deterministic ordering; Go map iteration is non-deterministic
		// and would cause markdown files to be included in a different order each run,
		// invalidating the LLM prefix cache.
		sortedRootDirs := make([]string, 0, len(rootPkgDirs))
		for dir := range rootPkgDirs {
			sortedRootDirs = append(sortedRootDirs, dir)
		}
		slices.SortStableFunc(sortedRootDirs, cmp.Compare)
		for _, dir := range sortedRootDirs {
			pkg := rootPkgDirs[dir]
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			// Sort entries by name for deterministic ordering.
			// Without sorting, the filesystem order could change when files are added/removed,
			// shifting the position of existing markdown files in the prompt and breaking
			// the LLM prefix cache.
			slices.SortStableFunc(entries, func(a, b os.DirEntry) int {
				return strings.Compare(a.Name(), b.Name())
			})
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				lowerName := strings.ToLower(name)
				if strings.HasSuffix(lowerName, ".md") && !strings.HasPrefix(lowerName, "_") {
					path := filepath.Join(dir, name)
					if !seenFilePaths[path] {
						if _, ok := nonGoFilePaths[path]; !ok {
							nonGoFilePaths[path] = pkg
							logger.Info("include markdown file", "path", path)
						}
					}
				}
			}
		}

		// Sort nonGoFilePaths for deterministic ordering; Go map iteration is non-deterministic
		// and would cause non-Go files to be included in a different order each run,
		// invalidating the LLM prefix cache.
		sortedNonGoPaths := make([]string, 0, len(nonGoFilePaths))
		for path := range nonGoFilePaths {
			sortedNonGoPaths = append(sortedNonGoPaths, path)
		}
		slices.SortStableFunc(sortedNonGoPaths, cmp.Compare)

		// Read non-Go files in parallel to reduce I/O latency.
		// Results are stored in an indexed slice to preserve the sorted order
		// established by sortedNonGoPaths, ensuring deterministic output.
		nonGoResults := make([]*File, len(sortedNonGoPaths))
		var nonGoWg sync.WaitGroup
		nonGoSem := make(chan struct{}, 16) // bounded concurrency for I/O-bound work

		for i, path := range sortedNonGoPaths {
			nonGoWg.Add(1)
			nonGoSem <- struct{}{}
			go func(i int, path string) {
				defer nonGoWg.Done()
				defer func() { <-nonGoSem }()

				pkg := nonGoFilePaths[path]

				content, err := os.ReadFile(path)
				if err != nil {
					logger.Warn("cannot read non-go file", "path", path, "error", err)
					return
				}
				info, _ := os.Stat(path)

				// check if text file
				mime := mimetype.Detect(content)
				if !strings.HasPrefix(mime.String(), "text/") {
					if mime.String() == "application/octet-stream" {
						// unknown, check for null bytes.
						// Only scan the first 8KB: text files never contain null
						// bytes and binary files have them in the first few
						// bytes, so scanning the full content is wasteful for
						// large text files.
						checkLen := min(len(content), 8192)
						if bytes.Contains(content[:checkLen], []byte{0}) {
							return // binary
						}
					} else {
						// not text
						return
					}
				}

				f := &File{
					Path:                    path,
					IsGoFile:                false,
					Content:                 content,
					Package:                 pkg,
					PackageIsRoot:           rootPkgSet[pkg],
					PackageDistanceFromRoot: 0, // overwritten by simplify.go
					PackagePathDepth:        len(strings.Split(pkg.PkgPath, "/")),
					Module:                  pkg.Module,
					ModuleIsRoot:            pkg.Module != nil && rootModulePaths[pkg.Module.Path],
					ModuleIsNil:             pkg.Module == nil,
					IsEmbed:                 embedFilePaths[path],
				}
				if info != nil {
					f.ModTime = info.ModTime()
				}
				nonGoResults[i] = f
			}(i, path)
		}
		nonGoWg.Wait()

		for _, f := range nonGoResults {
			if f != nil {
				files = append(files, f)
			}
		}

		// Sort files into stable tiers for LLM prefix cache reuse.
		// See TheoryOfFileOrdering.
		slices.SortStableFunc(files, func(a, b *File) int {
			// root module last — outermost grouping so that all non-root-module
			// files (dependencies, stdlib) form the stable prefix, maximizing
			// prefix cache reuse across requests that change only root-module
			// files.
			if !a.ModuleIsRoot && b.ModuleIsRoot {
				return -1
			} else if a.ModuleIsRoot && !b.ModuleIsRoot {
				return 1
			}

			// non-nil module last (nil modules like stdlib come first within
			// the non-root-module group)
			if a.ModuleIsNil && !b.ModuleIsNil {
				return -1
			} else if !a.ModuleIsNil && b.ModuleIsNil {
				return 1
			}

			// root package last — within each module group, context files
			// (non-root packages) precede focus files (root package).
			if !a.PackageIsRoot && b.PackageIsRoot {
				return -1
			} else if a.PackageIsRoot && !b.PackageIsRoot {
				return 1
			}

			// go files last
			if !a.IsGoFile && b.IsGoFile {
				return -1
			} else if a.IsGoFile && !b.IsGoFile {
				return 1
			}

			// low distance last
			if a.PackageDistanceFromRoot != b.PackageDistanceFromRoot {
				return -cmp.Compare(a.PackageDistanceFromRoot, b.PackageDistanceFromRoot)
			}

			// shallow package last
			if a.PackagePathDepth != b.PackagePathDepth {
				return -cmp.Compare(a.PackagePathDepth, b.PackagePathDepth)
			}

			// package path alphabetical
			if a.Package.PkgPath != b.Package.PkgPath {
				return cmp.Compare(a.Package.PkgPath, b.Package.PkgPath)
			}

			// file path alphabetical — primary stable key.
			// Using modification time before path would cause reordering
			// when timestamps change without content change (e.g., after git checkout),
			// destroying the LLM prefix cache.
			if a.Path != b.Path {
				return cmp.Compare(a.Path, b.Path)
			}
			// modification time — final tiebreaker for identical paths (should never happen)
			if a.ModTime.Before(b.ModTime) {
				return -1
			} else if b.ModTime.Before(a.ModTime) {
				return 1
			}
			return 0
		})

		return
	})
}

func formatContentForPrompt(w io.Writer, content []byte, isRoot bool, readOnly bool, path string) error {
	prefix := "focus file"
	if !isRoot {
		prefix = "context file"
	}
	readOnlyNote := ""
	if readOnly {
		readOnlyNote = " (read-only)"
	}
	_, err := fmt.Fprint(w, "``` begin of "+prefix+" "+path+readOnlyNote+"\n")
	if err != nil {
		return err
	}

	_, err = w.Write(content)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "\n")
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(w, "``` end of "+prefix+" "+path+"\n\n")
	if err != nil {
		return err
	}

	return nil
}
