package gocodes

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

var includeStdLib = cmds.Switch("-include-std")

type IncludeStdLib bool

func (Module) IncludeStdLib() IncludeStdLib {
	return IncludeStdLib(*includeStdLib)
}

const FileOrderingTheory = `
Files are sorted so that stable context files (dependencies, non-root packages) appear
first and volatile focus files (root package) appear last. This ordering maximizes the
common prefix between consecutive requests: when only focus files change, all preceding
context files remain identical, allowing LLM prefix caching to reuse cached key-value
states for unchanged content.

Within each priority group, files are ordered by their path as the primary key. This
ensures a fully deterministic order that is independent of modification times. Using
modification times would cause reordering whenever timestamps change (e.g., after a
fresh checkout or touch), destroying the entire prefix cache. Path-based ordering
guarantees that unchanged files always appear in the same position, maximizing cache
reuse across requests. Modification time is kept as a final tiebreaker for hypothetical
cases where two files could share the same path (impossible in practice).
`

type File struct {
	Path                    string
	IsGoFile                bool
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
	// DefinedObjects is unused under the lightweight loader: NeedTypesInfo is
	// not loaded, so no object map is populated. Kept nil for APIs that still
	// read the field.
	DefinedObjects map[types.Object]bool
	DoNotSimplify  bool

	transformCond *sync.Cond
	Transform     *Transform
	Pending       *Transformed
	Confirmed     *Transformed
	ModTime       time.Time
}

type Transformed struct {
	What      string
	Ast       *ast.File
	Content   []byte
	NumTokens int
}

type MaxPackageDistanceFromRoot int

var _ configs.Configurable = MaxPackageDistanceFromRoot(0)

func (m MaxPackageDistanceFromRoot) TaigoConfigurable() {
}

func (Module) MaxPackageDistanceFromRoot(
	loader configs.Loader,
) MaxPackageDistanceFromRoot {
	return vars.FirstNonZero(
		configs.First[MaxPackageDistanceFromRoot](loader, "go.max_distance"),
		2, // default
	)
}

type GetFiles func() ([]*File, error)

func (Module) Files(
	getFileSet GetFileSet,
	getRootPackages GetRootPackages,
	getContextPackages GetContextPackages,
	logger logs.Logger,
	maxDistance MaxPackageDistanceFromRoot,
	includeStdLib IncludeStdLib,
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

		// packageDistanceFromRoot records the shortest import distance from
		// any root or context package. Computed via BFS over the Imports graph
		// populated by NeedDeps in a single packages.Load call.
		// See TheoryOfLightweightPackageLoading in packages.go.
		packageDistanceFromRoot := make(map[*packages.Package]int)
		for _, pkg := range rootPkgs {
			packageDistanceFromRoot[pkg] = 0
		}
		for _, pkg := range contextPkgs {
			if _, ok := packageDistanceFromRoot[pkg]; !ok {
				packageDistanceFromRoot[pkg] = 0
			}
		}

		// BFS to compute distances up to maxDistance. With NeedDeps, all
		// transitive dependencies are already loaded and accessible via
		// pkg.Imports, so no additional packages.Load calls are needed.
		queue := append([]*packages.Package{}, rootPkgs...)
		queue = append(queue, contextPkgs...)
		for len(queue) > 0 {
			pkg := queue[0]
			queue = queue[1:]
			d := packageDistanceFromRoot[pkg]
			if d >= int(maxDistance) {
				continue
			}
			for _, imp := range pkg.Imports {
				if imp == nil {
					continue
				}
				if _, ok := packageDistanceFromRoot[imp]; !ok {
					packageDistanceFromRoot[imp] = d + 1
					queue = append(queue, imp)
				}
			}
		}

		// Collect all packages within the distance bound.
		allPkgs := make([]*packages.Package, 0, len(packageDistanceFromRoot))
		for pkg, d := range packageDistanceFromRoot {
			if d <= int(maxDistance) {
				allPkgs = append(allPkgs, pkg)
			}
		}

		// rootPkgSet provides O(1) root package membership checks.
		rootPkgSet := make(map[*packages.Package]bool, len(rootPkgs))
		for _, pkg := range rootPkgs {
			rootPkgSet[pkg] = true
		}

		// root modules
		rootModulePaths := make(map[string]bool)
		for _, pkg := range allPkgs {
			if pkg.Module != nil {
				rootModulePaths[pkg.Module.Path] = true
			}
			if *debug {
				logger.Info("loaded package", "path", pkg.PkgPath)
			}
		}

		// Discover Go files from pkg.GoFiles and parse individually.
		// Only files within maxDistance are parsed, avoiding OOM from parsing
		// all transitive dependency ASTs. See TheoryOfLightweightPackageLoading
		// in packages.go for the rationale.
		seenFilePaths := make(map[string]bool)
		type goFileEntry struct {
			path string
			pkg  *packages.Package
		}
		var allGoFiles []goFileEntry
		for _, pkg := range allPkgs {
			for _, path := range pkg.GoFiles {
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

		for _, entry := range allGoFiles {
			pkg := entry.pkg
			distance := packageDistanceFromRoot[pkg]
			// Filter early: skip files beyond maxDistance or in stdlib
			// without -include-std, avoiding unnecessary parsing and memory.
			if distance > int(maxDistance) {
				continue
			}
			if pkg.Module == nil && !includeStdLib {
				continue
			}

			path := entry.path
			f := &File{
				Path:          path,
				IsGoFile:      true,
				transformCond: sync.NewCond(new(sync.Mutex)),
			}
			if info, err := os.Stat(path); err == nil {
				f.ModTime = info.ModTime()
			}

			// Parse the file individually. This replaces the previous
			// approach of relying on pkg.Syntax (which required NeedSyntax
			// and retained all ASTs in memory).
			src, err := os.ReadFile(path)
			if err != nil {
				logger.Warn("cannot read go file", "path", path, "error", err)
				continue
			}
			astFile, err := parser.ParseFile(fset, path, src, parser.ParseComments)
			if err != nil {
				logger.Warn("cannot parse go file", "path", path, "error", err)
				continue
			}
			f.TokenFile = fset.File(astFile.Pos())
			f.AstFile = astFile

			f.Package = pkg
			f.PackageIsRoot = rootPkgSet[pkg]
			f.PackageDistanceFromRoot = distance
			f.PackagePathDepth = len(strings.Split(pkg.PkgPath, "/"))
			f.Module = pkg.Module
			f.ModuleIsRoot = pkg.Module != nil && rootModulePaths[pkg.Module.Path]
			f.ModuleIsNil = pkg.Module == nil

			// DefinedObjects requires NeedTypesInfo, which is no longer loaded
			// to reduce memory. Skip when TypesInfo is nil.
			if pkg.TypesInfo != nil {
				f.DefinedObjects = make(map[types.Object]bool)
				for ident, obj := range pkg.TypesInfo.Defs {
					if ident.Pos().IsValid() && fset.File(ident.Pos()) == f.TokenFile {
						f.DefinedObjects[obj] = true
					}
				}
			}

			files = append(files, f)
		}

		// collect non-Go files
		nonGoFilePaths := make(map[string]*packages.Package)
		for _, pkg := range allPkgs {
			allFiles := [][]string{
				pkg.EmbedFiles,
				pkg.OtherFiles,
			}
			for _, fileList := range allFiles {
				for _, path := range fileList {
					if seenFilePaths[path] {
						continue
					}
					if _, ok := nonGoFilePaths[path]; !ok {
						nonGoFilePaths[path] = pkg
					}
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
				// Filter early: skip files beyond maxDistance or in stdlib
				// without -include-std, avoiding reading unnecessary content.
				distance := packageDistanceFromRoot[pkg]
				if distance > int(maxDistance) {
					return
				}
				if pkg.Module == nil && !includeStdLib {
					return
				}

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
						checkLen := len(content)
						if checkLen > 8192 {
							checkLen = 8192
						}
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
					PackageDistanceFromRoot: distance,
					PackagePathDepth:        len(strings.Split(pkg.PkgPath, "/")),
					Module:                  pkg.Module,
					ModuleIsRoot:            pkg.Module != nil && rootModulePaths[pkg.Module.Path],
					ModuleIsNil:             pkg.Module == nil,
					transformCond:           sync.NewCond(new(sync.Mutex)),
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

		// sort
		slices.SortStableFunc(files, func(a, b *File) int {
			// root package last
			if !a.PackageIsRoot && b.PackageIsRoot {
				return -1
			} else if a.PackageIsRoot && !b.PackageIsRoot {
				return 1
			}

			// root module last
			if !a.ModuleIsRoot && b.ModuleIsRoot {
				return -1
			} else if a.ModuleIsRoot && !b.ModuleIsRoot {
				return 1
			}

			// non-nil module last
			if a.ModuleIsNil && !b.ModuleIsNil {
				return -1
			} else if !a.ModuleIsNil && b.ModuleIsNil {
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

// formatBufPool reuses bytes.Buffer instances across concurrent transform
// workers to reduce GC pressure during the simplification pipeline.
var formatBufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

func formatASTForPrompt(w io.Writer, fileAst *ast.File, fset *token.FileSet, isRoot bool, path string, skipImports bool) error {
	if isRoot {
		_, err := fmt.Fprint(w, "``` begin of focus file "+path+"\n")
		if err != nil {
			return err
		}
	} else {
		_, err := fmt.Fprint(w, "``` begin of context file "+path+"\n")
		if err != nil {
			return err
		}
	}

	buf := formatBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer formatBufPool.Put(buf)

	err := format.Node(buf, fset, fileAst)
	if err != nil {
		panic(err)
	}

	var res []byte
	if skipImports {
		// Comment deletion does not change import usage, so goimports
		// would be a no-op. Skip it to avoid redundant parsing.
		res = buf.Bytes()
	} else {
		res, err = imports.Process(path, buf.Bytes(), nil)
		if err != nil {
			res = buf.Bytes()
		}
	}

	_, err = w.Write(res)
	if err != nil {
		return err
	}
	if !bytes.HasSuffix(res, []byte("\n")) {
		_, err = fmt.Fprintf(w, "\n")
		if err != nil {
			return err
		}
	}

	if isRoot {
		_, err := fmt.Fprint(w, "``` end of focus file "+path+"\n\n")
		if err != nil {
			return err
		}
	} else {
		_, err := fmt.Fprint(w, "``` end of context file "+path+"\n\n")
		if err != nil {
			return err
		}
	}

	return nil
}

func formatContentForPrompt(w io.Writer, content []byte, isRoot bool, path string) error {
	if isRoot {
		_, err := fmt.Fprint(w, "``` begin of focus file "+path+"\n")
		if err != nil {
			return err
		}
	} else {
		_, err := fmt.Fprint(w, "``` begin of context file "+path+"\n")
		if err != nil {
			return err
		}
	}

	_, err := w.Write(content)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "\n")
	if err != nil {
		return err
	}

	if isRoot {
		_, err := fmt.Fprint(w, "``` end of focus file "+path+"\n\n")
		if err != nil {
			return err
		}
	} else {
		_, err := fmt.Fprint(w, "``` end of context file "+path+"\n\n")
		if err != nil {
			return err
		}
	}

	return nil
}
