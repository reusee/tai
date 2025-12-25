package taigo

import (
	"go/parser"
	"go/token"
	"io"

	"github.com/reusee/tai/taivm"
)

func NewVM(name string, source io.Reader, options *Options) (*taivm.VM, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	mainFunc, err := compile(file)
	if err != nil {
		return nil, err
	}

	vm := taivm.NewVM(mainFunc)

	registerBuiltins(vm, options)

	return vm, nil
}
