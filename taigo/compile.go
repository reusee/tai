package taigo

import (
	"fmt"
	"go/ast"

	"github.com/reusee/tai/taivm"
)

func compile(file *ast.File) (*taivm.Function, error) {
	c := &compiler{
		name:       "main",
		consts:     make(map[any]int),
		locals:     make(map[string]int),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}
	if err := c.compileFile(file); err != nil {
		return nil, err
	}
	// Implicit return at end of script
	c.loadConst(nil)
	c.emit(taivm.OpReturn)
	for name, indices := range c.unresolved {
		target, ok := c.labels[name]
		if !ok {
			return nil, fmt.Errorf("label %s not defined", name)
		}
		for _, idx := range indices {
			c.patchJump(idx, target)
		}
	}
	return c.getFunction(), nil
}
