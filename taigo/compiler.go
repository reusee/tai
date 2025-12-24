package taigo

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

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

		for _, stmt := range d.Body.List {
			if err := sub.compileStmt(stmt); err != nil {
				return err
			}
		}

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

func (c *compiler) compileExpr(expr ast.Expr) error {
	switch e := expr.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.INT:
			v, err := strconv.ParseInt(e.Value, 0, 64)
			if err != nil {
				return err
			}
			c.loadConst(v)
		case token.FLOAT:
			v, err := strconv.ParseFloat(e.Value, 64)
			if err != nil {
				return err
			}
			c.loadConst(v)
		case token.STRING:
			v, err := strconv.Unquote(e.Value)
			if err != nil {
				return err
			}
			c.loadConst(v)
		case token.CHAR:
			v, _, _, err := strconv.UnquoteChar(e.Value, '\'')
			if err != nil {
				return err
			}
			c.loadConst(int64(v))
		default:
			return fmt.Errorf("unknown basic lit kind: %v", e.Kind)
		}

	case *ast.Ident:
		switch e.Name {
		case "true":
			c.loadConst(true)
		case "false":
			c.loadConst(false)
		case "nil":
			c.loadConst(nil)
		default:
			idx := c.addConst(e.Name)
			c.emit(taivm.OpLoadVar.With(idx))
		}

	case *ast.ParenExpr:
		return c.compileExpr(e.X)

	default:
		return fmt.Errorf("unknown expr type: %T", expr)
	}
	return nil
}

func (c *compiler) compileStmt(stmt ast.Stmt) error {
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if err := c.compileExpr(s.X); err != nil {
			return err
		}
		c.emit(taivm.OpPop)

	case *ast.BlockStmt:
		c.emit(taivm.OpEnterScope)
		for _, stmt := range s.List {
			if err := c.compileStmt(stmt); err != nil {
				return err
			}
		}
		c.emit(taivm.OpLeaveScope)

	case *ast.ReturnStmt:
		if len(s.Results) == 0 {
			c.loadConst(nil)
			c.emit(taivm.OpReturn)
		} else if len(s.Results) == 1 {
			if err := c.compileExpr(s.Results[0]); err != nil {
				return err
			}
			c.emit(taivm.OpReturn)
		} else {
			for _, r := range s.Results {
				if err := c.compileExpr(r); err != nil {
					return err
				}
			}
			c.emit(taivm.OpMakeTuple.With(len(s.Results)))
			c.emit(taivm.OpReturn)
		}

	default:
		return fmt.Errorf("unknown stmt type: %T", stmt)
	}
	return nil
}
