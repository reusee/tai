package taigo

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"

	"github.com/reusee/tai/taivm"
)

func Exec(vm *taivm.VM, src any) (any, error) {
	var srcStr string
	switch s := src.(type) {
	case string:
		srcStr = s
	case []byte:
		srcStr = string(s)
	case io.Reader:
		b, err := io.ReadAll(s)
		if err != nil {
			return nil, err
		}
		srcStr = string(b)
	default:
		srcStr = fmt.Sprint(s)
	}

	fset := token.NewFileSet()
	expr, err := parser.ParseExpr(srcStr)
	if err == nil {
		fn, err := compileExpr(expr)
		if err != nil {
			return nil, err
		}
		return runInVM(vm, fn)
	}

	file, err := parser.ParseFile(fset, "exec", srcStr, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	pkg, err := compile(file)
	if err != nil {
		return nil, err
	}
	return runInVM(vm, pkg.Init)
}

func runInVM(vm *taivm.VM, fn *taivm.Function) (any, error) {
	newVM := taivm.NewVM(fn)
	newVM.Scope = vm.Scope
	for _, err := range newVM.Run {
		if err != nil {
			return nil, err
		}
	}
	if newVM.SP > 0 {
		return newVM.OperandStack[newVM.SP-1], nil
	}
	return nil, nil
}
