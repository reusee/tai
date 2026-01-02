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
	"slices"
	"strings"
	"sync"

	"github.com/gabriel-vasile/mimetype"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
	"golang.org/x/tools/go/packages"
)

var includeStdLib = cmds.Switch("-include-std")

type IncludeStdLib bool

func (Module) IncludeStdLib() IncludeStdLib {
	return IncludeStdLib(*includeStdLib)
}

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

	transformCond *sync.Cond
	Transform     *Transform
	Pending       *Transformed
	Confirmed     *Transformed
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
	getPackages GetPackages,
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
			files = append(files, f)
			tokenFileToFile[file] = f
		}

		// packages
		pkgs, err := getPackages()
		if err != nil {
			return nil, err
		}

		// root modules
		rootModulePaths := make(map[string]bool)
		for _, pkg := range pkgs {
			if pkg.Module != nil {
				rootModulePaths[pkg.Module.Path] = true
			}
			if *debug {
				logger.Info("root package", "path", pkg.PkgPath)
			}
		}

		packageDistanceFromRoot := make(map[*packages.Package]int)
		queue := []*packages.Package{}
		for _, pkg := range pkgs {
			packageDistanceFromRoot[pkg] = 0
			queue = append(queue, pkg)
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
		packages.Visit(pkgs, nil, func(pkg *packages.Package) {
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
		for _, file := range files {
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
			file.PackageIsRoot = slices.Contains(pkgs, file.Package)
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
		packages.Visit(pkgs, nil, func(pkg *packages.Package) {
			allFiles := [][]string{
				pkg.EmbedFiles,
				pkg.OtherFiles,
			}
			for _, fileList := range allFiles {
				for _, path := range fileList {
					if _, ok := nonGoFilePaths[path]; !ok {
						nonGoFilePaths[path] = pkg
					}
				}
			}
		})

		for path, pkg := range nonGoFilePaths {
			content, err := os.ReadFile(path)
			if err != nil {
				logger.Warn("cannot read non-go file", "path", path, "error", err)
				continue
			}

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
				PackageIsRoot:           slices.Contains(pkgs, pkg),
				PackageDistanceFromRoot: packageDistanceFromRoot[pkg],
				PackagePathDepth:        len(strings.Split(pkg.PkgPath, "/")),
				Module:                  pkg.Module,
				ModuleIsRoot:            pkg.Module != nil && rootModulePaths[pkg.Module.Path],
				ModuleIsNil:             pkg.Module == nil,
				transformCond:           sync.NewCond(new(sync.Mutex)),
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

			// package name
			if a.Package.PkgPath != b.Package.PkgPath {
				return cmp.Compare(a.Package.PkgPath, b.Package.PkgPath)
			}

			// large file last
			aSize := 0
			if a.TokenFile != nil {
				aSize = a.TokenFile.Size()
			} else {
				aSize = len(a.Content)
			}
			bSize := 0
			if b.TokenFile != nil {
				bSize = b.TokenFile.Size()
			} else {
				bSize = len(b.Content)
			}
			return cmp.Compare(aSize, bSize)
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

	err := format.Node(w, fset, fileAST)
	if err != nil {
		panic(err)
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
