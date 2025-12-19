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

	return vm, nil
}
