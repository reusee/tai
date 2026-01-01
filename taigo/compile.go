package taigo

import (
	"fmt"
	"go/ast"

	"github.com/reusee/tai/taivm"
)

func compile(files ...*ast.File) (*Package, error) {
	c := &compiler{
		consts:     make(map[any]int),
		locals:     make(map[string]variable),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}
	if err := c.compileFiles(files); err != nil {
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
	return c.getPackage(), nil
}

func compileExpr(expr ast.Expr) (*taivm.Function, error) {
	c := &compiler{
		name:       "eval",
		consts:     make(map[any]int),
		locals:     make(map[string]variable),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}
	if _, err := c.compileExpr(expr); err != nil {
		return nil, err
	}
	c.emit(taivm.OpReturn)
	if err := c.resolveLabels(); err != nil {
		return nil, err
	}
	return c.getFunction(), nil
}
