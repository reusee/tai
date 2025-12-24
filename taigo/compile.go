package taigo

import (
	"go/ast"

	"github.com/reusee/tai/taivm"
)

func compile(file *ast.File) (*taivm.Function, error) {
	c := &compiler{
		name: "main",
	}
	if err := c.compileFile(file); err != nil {
		return nil, err
	}
	// Implicit return at end of script
	c.loadConst(nil)
	c.emit(taivm.OpReturn)
	return c.getFunction(), nil
}
