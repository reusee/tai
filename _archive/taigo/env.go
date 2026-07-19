package taigo

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/reusee/tai/taivm"
)

type Env struct {
	Globals map[string]any

	Stdin  io.Reader // if nil, default to os.Stdin
	Stdout io.Writer // if nil, default to os.Stdout
	Stderr io.Writer // if nil, default to os.Stderr

	FilePatterns []string
	Source       any
	SourceName   string
	FileSet      *token.FileSet
	Files        []*ast.File
	Package      *Package
}

func (e *Env) NewVM() (*taivm.VM, error) {
	pkg, err := e.GetPackage()
	if err != nil {
		return nil, err
	}

	vm := taivm.NewVM(pkg.Init)

	e.registerBuiltins(vm)
	for key, val := range e.Globals {
		var t *taivm.Type
		if rt, ok := val.(reflect.Type); ok {
			t = taivm.FromReflectType(rt)
			if rt.PkgPath() != "" && t.Kind != taivm.KindExternal {
				t2 := *t
				t2.Kind = taivm.KindExternal
				t2.External = rt
				t = &t2
			}
		} else if tt, ok := val.(*taivm.Type); ok {
			t = tt
		}
		if t != nil {
			if t.Name != key {
				t2 := *t
				t2.Name = key
				t = &t2
			}
			vm.Def(key, t)
		} else if val != nil {
			rt := reflect.TypeOf(val)
			t = taivm.FromReflectType(rt)
			if rt.PkgPath() != "" && t.Kind != taivm.KindExternal {
				t2 := *t
				t2.Kind = taivm.KindExternal
				t2.External = rt
				t = &t2
			}
			vm.DefWithType(key, val, t)
		} else {
			vm.Def(key, val)
		}
	}

	return vm, nil
}

func (e *Env) RunVM() (*taivm.VM, error) {
	vm, err := e.NewVM()
	if err != nil {
		return nil, err
	}
	for _, err := range vm.Run {
		if err != nil {
			return nil, err
		}
	}
	return vm, nil
}

func (e *Env) GetPackage() (*Package, error) {
	if e.Package != nil {
		return e.Package, nil
	}

	files, err := e.GetFiles()
	if err != nil {
		return nil, err
	}

	externalTypes := make(map[string]*taivm.Type)
	externalValueTypes := make(map[string]*taivm.Type)
	for name, val := range e.Globals {
		var t *taivm.Type
		if rt, ok := val.(reflect.Type); ok {
			t = taivm.FromReflectType(rt)
			if rt.PkgPath() != "" && t.Kind != taivm.KindExternal {
				t2 := *t
				t2.Kind = taivm.KindExternal
				t2.External = rt
				t = &t2
			}
		} else if tt, ok := val.(*taivm.Type); ok {
			t = tt
		}
		if t != nil {
			if t.Name != name {
				t2 := *t
				t2.Name = name
				t = &t2
			}
			externalTypes[name] = t
		} else if val != nil {
			rt := reflect.TypeOf(val)
			t := taivm.FromReflectType(rt)
			if rt.PkgPath() != "" && t.Kind != taivm.KindExternal {
				t2 := *t
				t2.Kind = taivm.KindExternal
				t2.External = rt
				t = &t2
			}
			externalValueTypes[name] = t
		}
	}

	pkg, err := compile(externalTypes, externalValueTypes, files...)
	if err != nil {
		return nil, err
	}

	e.Package = pkg
	return pkg, nil
}

func (e *Env) GetFiles() ([]*ast.File, error) {
	if e.Files != nil {
		return e.Files, nil
	}

	fset := e.GetFileSet()

	if e.Source != nil {
		file, err := parser.ParseFile(fset, e.SourceName, e.Source, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		e.Files = []*ast.File{file}
		return e.Files, nil
	}

	if len(e.FilePatterns) > 0 {
		for _, pattern := range e.FilePatterns {
			filePaths, err := filepath.Glob(pattern)
			if err != nil {
				return nil, err
			}
			for _, filePath := range filePaths {
				content, err := os.ReadFile(filePath)
				if err != nil {
					return nil, err
				}
				file, err := parser.ParseFile(fset, filePath, content, parser.SkipObjectResolution)
				if err != nil {
					return nil, err
				}
				e.Files = append(e.Files, file)
			}
		}
		if len(e.Files) == 0 {
			return nil, errors.New("no source")
		}
		return e.Files, nil
	}

	return nil, errors.New("no source")
}

func (e *Env) GetFileSet() *token.FileSet {
	if e.FileSet != nil {
		return e.FileSet
	}

	fset := token.NewFileSet()

	e.FileSet = fset
	return fset
}
