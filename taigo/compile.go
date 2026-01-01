package taigo

import (
	"go/ast"

	"github.com/reusee/tai/taivm"
)

func compile(externalTypes, externalValueTypes map[string]*taivm.Type, files ...*ast.File) (*Package, error) {
	c := &compiler{
		consts:       make(map[any]int),
		locals:       make(map[string]variable),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		structFields: make(map[string][]string),
		types:        make(map[string]*taivm.Type),
		globals:      make(map[string]*taivm.Type),
	}
	c.initExternal(externalTypes, externalValueTypes)
	if err := c.compileFiles(files); err != nil {
		return nil, err
	}
	// Implicit return at end of script
	c.loadConst(nil)
	c.emit(taivm.OpReturn)
	if err := c.resolveLabels(); err != nil {
		return nil, err
	}
	return c.getPackage(), nil
}

func compileExpr(expr ast.Expr, externalTypes, externalValueTypes map[string]*taivm.Type) (*taivm.Function, error) {
	c := &compiler{
		name:         "eval",
		consts:       make(map[any]int),
		locals:       make(map[string]variable),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		structFields: make(map[string][]string),
		types:        make(map[string]*taivm.Type),
		globals:      make(map[string]*taivm.Type),
	}
	c.initExternal(externalTypes, externalValueTypes)
	if _, err := c.compileExpr(expr); err != nil {
		return nil, err
	}
	c.emit(taivm.OpReturn)
	if err := c.resolveLabels(); err != nil {
		return nil, err
	}
	return c.getFunction(), nil
}
