package taipy

import (
	"fmt"

	"github.com/reusee/tai/taivm"
	"go.starlark.net/syntax"
)

type compiler struct {
	name      string
	code      []taivm.OpCode
	constants []any
	constMap  map[any]int
}

func newCompiler(name string) *compiler {
	return &compiler{
		name:     name,
		constMap: make(map[any]int),
	}
}

func (c *compiler) toFunction() *taivm.Function {
	return &taivm.Function{
		Name:      c.name,
		Code:      c.code,
		Constants: c.constants,
	}
}

func (c *compiler) addConst(val any) int {
	if isComparable(val) {
		if idx, ok := c.constMap[val]; ok {
			return idx
		}
	}
	idx := len(c.constants)
	c.constants = append(c.constants, val)
	if isComparable(val) {
		c.constMap[val] = idx
	}
	return idx
}

func isComparable(v any) bool {
	switch v.(type) {
	case int, int64, float64, string, bool, nil, taivm.Symbol:
		return true
	}
	return false
}

func (c *compiler) emit(op taivm.OpCode) {
	c.code = append(c.code, op)
}

func (c *compiler) currentIP() int {
	return len(c.code)
}

func (c *compiler) patchJump(ip int, target int) {
	offset := target - ip - 1
	op := c.code[ip] & 0xff
	c.code[ip] = op.With(offset)
}

func (c *compiler) compileStmts(stmts []syntax.Stmt) error {
	for _, stmt := range stmts {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileStmt(stmt syntax.Stmt) error {
	switch s := stmt.(type) {
	case *syntax.ExprStmt:
		if err := c.compileExpr(s.X); err != nil {
			return err
		}
		c.emit(taivm.OpPop)
	case *syntax.AssignStmt:
		return c.compileAssign(s)
	case *syntax.DefStmt:
		return c.compileDef(s)
	case *syntax.ReturnStmt:
		if s.Result != nil {
			if err := c.compileExpr(s.Result); err != nil {
				return err
			}
		} else {
			c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
		}
		c.emit(taivm.OpReturn)
	case *syntax.IfStmt:
		return c.compileIf(s)
	case *syntax.WhileStmt:
		return c.compileWhile(s)
	case *syntax.BranchStmt:
		return fmt.Errorf("branch statements (break/continue) not yet implemented")
	default:
		return fmt.Errorf("unsupported statement type: %T", stmt)
	}
	return nil
}

func (c *compiler) compileAssign(s *syntax.AssignStmt) error {
	if s.Op != syntax.EQ {
		return fmt.Errorf("augmented assignment not supported yet")
	}

	switch lhs := s.LHS.(type) {
	case *syntax.Ident:
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(taivm.OpDefVar.With(c.addConst(lhs.Name)))
	case *syntax.IndexExpr:
		if err := c.compileExpr(lhs.X); err != nil {
			return err
		}
		if err := c.compileExpr(lhs.Y); err != nil {
			return err
		}
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(taivm.OpSetIndex)
	default:
		return fmt.Errorf("unsupported assignment target: %T", s.LHS)
	}
	return nil
}

func (c *compiler) compileExpr(expr syntax.Expr) error {
	switch e := expr.(type) {
	case *syntax.Literal:
		c.emit(taivm.OpLoadConst.With(c.addConst(e.Value)))
	case *syntax.Ident:
		c.emit(taivm.OpLoadVar.With(c.addConst(e.Name)))
	case *syntax.BinaryExpr:
		fnName, ok := binaryOpMap[e.Op]
		if !ok {
			return fmt.Errorf("unsupported binary op: %v", e.Op)
		}
		c.emit(taivm.OpLoadVar.With(c.addConst(fnName)))
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		if err := c.compileExpr(e.Y); err != nil {
			return err
		}
		c.emit(taivm.OpCall.With(2))
	case *syntax.CallExpr:
		if err := c.compileExpr(e.Fn); err != nil {
			return err
		}
		for _, arg := range e.Args {
			if _, ok := arg.(*syntax.BinaryExpr); ok {
				return fmt.Errorf("keyword arguments not supported yet")
			}
			if _, ok := arg.(*syntax.UnaryExpr); ok {
				return fmt.Errorf("star arguments not supported yet")
			}
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(taivm.OpCall.With(len(e.Args)))
	case *syntax.ListExpr:
		for _, elem := range e.List {
			if err := c.compileExpr(elem); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(e.List)))
	case *syntax.DictExpr:
		for _, entry := range e.List {
			entry := entry.(*syntax.DictEntry)
			if err := c.compileExpr(entry.Key); err != nil {
				return err
			}
			if err := c.compileExpr(entry.Value); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeMap.With(len(e.List)))
	case *syntax.IndexExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		if err := c.compileExpr(e.Y); err != nil {
			return err
		}
		c.emit(taivm.OpGetIndex)
	default:
		return fmt.Errorf("unsupported expression: %T", expr)
	}
	return nil
}

func (c *compiler) compileIf(s *syntax.IfStmt) error {
	if err := c.compileExpr(s.Cond); err != nil {
		return err
	}
	jumpFalseIP := c.currentIP()
	c.emit(taivm.OpJumpFalse)

	if err := c.compileStmts(s.True); err != nil {
		return err
	}

	jumpEndIP := c.currentIP()
	c.emit(taivm.OpJump)

	c.patchJump(jumpFalseIP, c.currentIP())

	if len(s.False) > 0 {
		if err := c.compileStmts(s.False); err != nil {
			return err
		}
	}

	c.patchJump(jumpEndIP, c.currentIP())
	return nil
}

func (c *compiler) compileWhile(s *syntax.WhileStmt) error {
	startIP := c.currentIP()

	if err := c.compileExpr(s.Cond); err != nil {
		return err
	}

	jumpExitIP := c.currentIP()
	c.emit(taivm.OpJumpFalse)

	if err := c.compileStmts(s.Body); err != nil {
		return err
	}

	loopIP := c.currentIP()
	offset := startIP - (loopIP + 1)
	c.emit(taivm.OpJump.With(offset))

	c.patchJump(jumpExitIP, c.currentIP())
	return nil
}

func (c *compiler) compileDef(s *syntax.DefStmt) error {
	sub := newCompiler(s.Name.Name)
	if err := sub.compileStmts(s.Body); err != nil {
		return err
	}
	sub.emit(taivm.OpLoadConst.With(sub.addConst(nil)))
	sub.emit(taivm.OpReturn)

	fn := sub.toFunction()
	fn.ParamNames = make([]string, len(s.Params))
	for i, p := range s.Params {
		if id, ok := p.(*syntax.Ident); ok {
			fn.ParamNames[i] = id.Name
		} else {
			return fmt.Errorf("complex parameters not supported")
		}
	}
	fn.NumParams = len(s.Params)

	c.emit(taivm.OpLoadConst.With(c.addConst(fn)))
	c.emit(taivm.OpMakeClosure)
	c.emit(taivm.OpDefVar.With(c.addConst(s.Name.Name)))

	return nil
}

var binaryOpMap = map[syntax.Token]string{
	syntax.PLUS:    "__add__",
	syntax.MINUS:   "__sub__",
	syntax.STAR:    "__mul__",
	syntax.SLASH:   "__div__",
	syntax.PERCENT: "__mod__",
	syntax.EQL:     "__eq__",
	syntax.NEQ:     "__ne__",
	syntax.LT:      "__lt__",
	syntax.GT:      "__gt__",
	syntax.LE:      "__le__",
	syntax.GE:      "__ge__",
}
