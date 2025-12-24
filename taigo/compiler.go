package taigo

import (
	"fmt"
	"go/ast"

	"github.com/reusee/tai/taivm"
)

type compiler struct {
	name      string
	code      []taivm.OpCode
	constants []any
}

func (c *compiler) getFunction() *taivm.Function {
	return &taivm.Function{
		Name:      c.name,
		Code:      c.code,
		Constants: c.constants,
	}
}

func (c *compiler) emit(op taivm.OpCode) {
	c.code = append(c.code, op)
}

func (c *compiler) addConst(val any) int {
	for i, v := range c.constants {
		if v == val {
			return i
		}
	}
	c.constants = append(c.constants, val)
	return len(c.constants) - 1
}

func (c *compiler) loadConst(val any) {
	idx := c.addConst(val)
	c.emit(taivm.OpLoadConst.With(idx))
}

func (c *compiler) compileFile(file *ast.File) error {
	for _, decl := range file.Decls {
		if err := c.compileDecl(decl); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileDecl(decl ast.Decl) error {
	switch d := decl.(type) {

	case *ast.FuncDecl:
		sub := &compiler{
			name: d.Name.Name,
		}

		// TODO compile body

		sub.loadConst(nil)
		sub.emit(taivm.OpReturn)

		fn := sub.getFunction()
		if d.Type.Params != nil {
			for _, field := range d.Type.Params.List {
				if len(field.Names) == 0 {
					fn.ParamNames = append(fn.ParamNames, "")
				} else {
					for _, name := range field.Names {
						fn.ParamNames = append(fn.ParamNames, name.Name)
					}
				}
			}
		}
		fn.NumParams = len(fn.ParamNames)

		idx := c.addConst(fn)
		c.emit(taivm.OpMakeClosure.With(idx))

		nameIdx := c.addConst(d.Name.Name)
		c.emit(taivm.OpDefVar.With(nameIdx))

	case *ast.GenDecl:
		// TODO imports, const, type, var

	default:
		return fmt.Errorf("unknown declaration type: %T", decl)

	}

	return nil
}
