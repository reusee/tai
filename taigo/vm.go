package taigo

import (
	"go/parser"
	"go/token"
	"io"

	"github.com/reusee/tai/taivm"
)

func NewVM(name string, source io.Reader) (*taivm.VM, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}

	// TODO compile file to Function and Env
	_ = file
	var mainFunc *taivm.Function
	var globals *taivm.Env

	vm := taivm.NewVM(mainFunc)
	vm.Scope = globals

	return vm, nil
}
