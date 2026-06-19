package gocodes

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/format"
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

Within each priority group, files are further ordered by modification time (oldest first)
so that recently edited files appear at the end of the group. This keeps the prefix of
older, unchanged files stable even when a few files are actively being edited.
The file path serves as the final deterministic tiebreaker.
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
	DefinedObjects          map[types.Object]bool
	DoNotSimplify           bool

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

		// fset
		fset, err := getFileSet()
		if err != nil {
			return nil, err
		}

		// info from file set
		tokenFileToFile := make(map[*token.File]*File)
		for file := range fset.Iterate {
			path := file.Name()
			if !strings.HasSuffix(path, ".go") {
				// non-Go file
				continue
			}
			f := &File{
				Path:          path,
				IsGoFile:      true,
				TokenFile:     file,
				transformCond: sync.NewCond(new(sync.Mutex)),
			}
			if info, err := os.Stat(path); err == nil {
				f.ModTime = info.ModTime()
			}
			files = append(files, f)
			tokenFileToFile[file] = f
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
		allPkgs := append(rootPkgs, contextPkgs...)

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

		packageDistanceFromRoot := make(map[*packages.Package]int)
		queue := []*packages.Package{}
		for _, pkg := range rootPkgs {
			packageDistanceFromRoot[pkg] = 0
			queue = append(queue, pkg)
		}
		for _, pkg := range contextPkgs {
			if _, ok := packageDistanceFromRoot[pkg]; !ok {
				packageDistanceFromRoot[pkg] = 0
				queue = append(queue, pkg)
			}
		}
		head := 0
		for head < len(queue) {
			currentPkg := queue[head]
			head++
			currentDistance := packageDistanceFromRoot[currentPkg]
			for _, importedPkg := range currentPkg.Imports {
				if existingDistance, ok := packageDistanceFromRoot[importedPkg]; !ok || currentDistance+1 < existingDistance {
					packageDistanceFromRoot[importedPkg] = currentDistance + 1
					queue = append(queue, importedPkg)
				}
			}
		}

		// mappings from packages
		tokenFileToAstFile := make(map[*token.File]*ast.File)
		astFileToPackage := make(map[*ast.File]*packages.Package)
		packages.Visit(allPkgs, nil, func(pkg *packages.Package) {
			for _, astFile := range pkg.Syntax {
				tokenFile := fset.File(astFile.Name.Pos())
				if tokenFile == nil {
					panic(fmt.Errorf("token file not found for %s.%s", pkg.PkgPath, astFile.Name))
				}
				tokenFileToAstFile[tokenFile] = astFile
				astFileToPackage[astFile] = pkg
			}
		})

		// files info
		seenPaths := make(map[string]bool)
		for _, file := range files {
			seenPaths[file.Path] = true
			if !file.IsGoFile {
				continue
			}
			astFile, ok := tokenFileToAstFile[file.TokenFile]
			if !ok {
				panic(fmt.Errorf("ast file not found for file %s", file.TokenFile.Name()))
			}
			file.AstFile = astFile
			pkg, ok := astFileToPackage[file.AstFile]
			if !ok {
				panic(fmt.Errorf("package not found for file %s", file.TokenFile.Name()))
			}
			file.Package = pkg
			file.PackageIsRoot = slices.Contains(rootPkgs, file.Package)
			file.PackageDistanceFromRoot = packageDistanceFromRoot[file.Package]
			file.PackagePathDepth = len(strings.Split(pkg.PkgPath, "/"))
			file.Module = file.Package.Module
			file.ModuleIsRoot = file.Module != nil && rootModulePaths[file.Module.Path]
			file.ModuleIsNil = file.Module == nil

			// Construct DefinedObjects
			file.DefinedObjects = make(map[types.Object]bool)
			for ident, obj := range file.Package.TypesInfo.Defs {
				if ident.Pos().IsValid() && fset.File(ident.Pos()) == file.TokenFile {
					file.DefinedObjects[obj] = true
				}
			}
		}

		// collect non-Go files
		nonGoFilePaths := make(map[string]*packages.Package)
		packages.Visit(allPkgs, nil, func(pkg *packages.Package) {
			allFiles := [][]string{
				pkg.EmbedFiles,
				pkg.OtherFiles,
			}
			for _, fileList := range allFiles {
				for _, path := range fileList {
					if seenPaths[path] {
						continue
					}
					if _, ok := nonGoFilePaths[path]; !ok {
						nonGoFilePaths[path] = pkg
					}
				}
			}
		})

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
		slices.Sort(sortedRootDirs)
		for _, dir := range sortedRootDirs {
			pkg := rootPkgDirs[dir]
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				lowerName := strings.ToLower(name)
				if strings.HasSuffix(lowerName, ".md") && !strings.HasPrefix(lowerName, "_") {
					path := filepath.Join(dir, name)
					if !seenPaths[path] {
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
		slices.Sort(sortedNonGoPaths)
		for _, path := range sortedNonGoPaths {
			pkg := nonGoFilePaths[path]
			content, err := os.ReadFile(path)
			if err != nil {
				logger.Warn("cannot read non-go file", "path", path, "error", err)
				continue
			}
			info, _ := os.Stat(path)

			// check if text file
			mime := mimetype.Detect(content)
			if !strings.HasPrefix(mime.String(), "text/") {
				if mime.String() == "application/octet-stream" {
					// unknown, check for null bytes
					if bytes.Contains(content, []byte{0}) {
						continue // binary
					}
				} else {
					// not text
					continue
				}
			}

			f := &File{
				Path:                    path,
				IsGoFile:                false,
				Content:                 content,
				Package:                 pkg,
				PackageIsRoot:           slices.Contains(rootPkgs, pkg),
				PackageDistanceFromRoot: packageDistanceFromRoot[pkg],
				PackagePathDepth:        len(strings.Split(pkg.PkgPath, "/")),
				Module:                  pkg.Module,
				ModuleIsRoot:            pkg.Module != nil && rootModulePaths[pkg.Module.Path],
				ModuleIsNil:             pkg.Module == nil,
				transformCond:           sync.NewCond(new(sync.Mutex)),
			}
			if info != nil {
				f.ModTime = info.ModTime()
			}
			files = append(files, f)
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

			// modification time: older files first so recently edited files appear last,
			// maximizing LLM prefix cache utilization across requests.
			if a.ModTime.Before(b.ModTime) {
				return -1
			} else if b.ModTime.Before(a.ModTime) {
				return 1
			}
			// file path alphabetical — final stable tiebreaker
			return cmp.Compare(a.Path, b.Path)
		})

		// filter
		filtered := files[:0]
		for _, file := range files {

			if file.Module == nil && !includeStdLib {
				// no module, stdlib
				continue
			}

			if file.PackageDistanceFromRoot > int(maxDistance) {
				// distance too far
				continue
			}

			filtered = append(filtered, file)
		}
		files = filtered

		return
	})
}

func formatASTForPrompt(w io.Writer, fileAST *ast.File, fset *token.FileSet, isRoot bool, path string) error {
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

	buf := new(bytes.Buffer)
	err := format.Node(buf, fset, fileAST)
	if err != nil {
		panic(err)
	}
	res, err := imports.Process(path, buf.Bytes(), nil)
	if err != nil {
		res = buf.Bytes()
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