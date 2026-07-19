package taipy

import (
	"io"

	"github.com/reusee/tai/taivm"
)

func NewVM(name string, source io.Reader) (*taivm.VM, error) {
	fn, err := Compile(name, source)
	if err != nil {
		return nil, err
	}

	vm := taivm.NewVM(fn)
	vm.Def("len", Len)
	vm.Def("range", Range)
	vm.Def("print", Print)
	vm.Def("struct", Struct)
	vm.Def("pow", Pow)
	vm.Def("abs", Abs)
	vm.Def("min", Min)
	vm.Def("max", Max)

	return vm, nil
}
