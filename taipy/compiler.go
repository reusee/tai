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
	loops     []*loopContext
}

type loopContext struct {
	continueIP int
	breakIPs   []int
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
	case *syntax.ForStmt:
		return c.compileFor(s)
	case *syntax.BranchStmt:
		return c.compileBranch(s)
	default:
		return fmt.Errorf("unsupported statement type: %T", stmt)
	}
	return nil
}

func (c *compiler) compileAssign(s *syntax.AssignStmt) error {
	if s.Op == syntax.EQ {
		switch lhs := s.LHS.(type) {
		case *syntax.Ident:
			if err := c.compileExpr(s.RHS); err != nil {
				return err
			}
			return c.compileStore(lhs)
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
		case *syntax.SliceExpr:
			if err := c.compileExpr(lhs.X); err != nil {
				return err
			}
			if lhs.Lo != nil {
				if err := c.compileExpr(lhs.Lo); err != nil {
					return err
				}
			} else {
				c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
			}
			if lhs.Hi != nil {
				if err := c.compileExpr(lhs.Hi); err != nil {
					return err
				}
			} else {
				c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
			}
			if lhs.Step != nil {
				if err := c.compileExpr(lhs.Step); err != nil {
					return err
				}
			} else {
				c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
			}
			if err := c.compileExpr(s.RHS); err != nil {
				return err
			}
			c.emit(taivm.OpSetSlice)
		case *syntax.DotExpr:
			if err := c.compileExpr(lhs.X); err != nil {
				return err
			}
			c.emit(taivm.OpLoadConst.With(c.addConst(lhs.Name.Name)))
			if err := c.compileExpr(s.RHS); err != nil {
				return err
			}
			c.emit(taivm.OpSetAttr)

		default:
			return fmt.Errorf("unsupported assignment target: %T", s.LHS)
		}
		return nil
	}

	var op taivm.OpCode
	switch s.Op {
	case syntax.PLUS_EQ:
		op = taivm.OpAdd
	case syntax.MINUS_EQ:
		op = taivm.OpSub
	case syntax.STAR_EQ:
		op = taivm.OpMul
	case syntax.SLASH_EQ:
		op = taivm.OpDiv
	case syntax.PERCENT_EQ:
		op = taivm.OpMod
	case syntax.AMP_EQ:
		op = taivm.OpBitAnd
	case syntax.PIPE_EQ:
		op = taivm.OpBitOr
	case syntax.CIRCUMFLEX_EQ:
		op = taivm.OpBitXor
	case syntax.LTLT_EQ:
		op = taivm.OpBitLsh
	case syntax.GTGT_EQ:
		op = taivm.OpBitRsh
	default:
		return fmt.Errorf("augmented assignment op %s not supported", s.Op)
	}

	switch lhs := s.LHS.(type) {
	case *syntax.Ident:
		c.emit(taivm.OpLoadVar.With(c.addConst(lhs.Name)))
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(op)
		return c.compileStore(lhs)

	case *syntax.IndexExpr:
		if err := c.compileExpr(lhs.X); err != nil {
			return err
		}
		if err := c.compileExpr(lhs.Y); err != nil {
			return err
		}
		c.emit(taivm.OpDup2)
		c.emit(taivm.OpGetIndex)
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(op)
		c.emit(taivm.OpSetIndex)

	case *syntax.DotExpr:
		if err := c.compileExpr(lhs.X); err != nil {
			return err
		}
		c.emit(taivm.OpLoadConst.With(c.addConst(lhs.Name.Name)))
		// Stack: [obj, attr]
		c.emit(taivm.OpDup2)
		// Stack: [obj, attr, obj, attr]
		c.emit(taivm.OpGetAttr)
		// Stack: [obj, attr, val]
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(op)
		// Stack: [obj, attr, new_val]
		c.emit(taivm.OpSetAttr)

	default:
		return fmt.Errorf("unsupported augmented assignment target: %T", s.LHS)
	}
	return nil
}

func (c *compiler) compileStore(lhs syntax.Expr) error {
	switch node := lhs.(type) {
	case *syntax.Ident:
		c.emit(taivm.OpDefVar.With(c.addConst(node.Name)))
		return nil
	default:
		return fmt.Errorf("unsupported variable type: %T", lhs)
	}
}

func (c *compiler) compileBranch(s *syntax.BranchStmt) error {
	if len(c.loops) == 0 {
		return fmt.Errorf("%s outside loop", s.Token.String())
	}
	loop := c.loops[len(c.loops)-1]

	switch s.Token {
	case syntax.BREAK:
		loop.breakIPs = append(loop.breakIPs, c.currentIP())
		c.emit(taivm.OpJump)
	case syntax.CONTINUE:
		ip := c.currentIP()
		c.emit(taivm.OpJump)
		c.patchJump(ip, loop.continueIP)
	case syntax.PASS:
		// no-op
	}
	return nil
}

func (c *compiler) compileExpr(expr syntax.Expr) error {
	switch e := expr.(type) {
	case *syntax.Literal:
		c.emit(taivm.OpLoadConst.With(c.addConst(e.Value)))
	case *syntax.Ident:
		c.emit(taivm.OpLoadVar.With(c.addConst(e.Name)))
	case *syntax.UnaryExpr:
		switch e.Op {
		case syntax.PLUS:
			return c.compileExpr(e.X)
		case syntax.MINUS:
			c.emit(taivm.OpLoadConst.With(c.addConst(0)))
			if err := c.compileExpr(e.X); err != nil {
				return err
			}
			c.emit(taivm.OpSub)
		case syntax.NOT:
			if err := c.compileExpr(e.X); err != nil {
				return err
			}
			c.emit(taivm.OpNot)
		case syntax.TILDE:
			if err := c.compileExpr(e.X); err != nil {
				return err
			}
			c.emit(taivm.OpBitNot)
		default:
			return fmt.Errorf("unsupported unary op: %v", e.Op)
		}
	case *syntax.BinaryExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		if err := c.compileExpr(e.Y); err != nil {
			return err
		}
		switch e.Op {
		case syntax.PLUS:
			c.emit(taivm.OpAdd)
		case syntax.MINUS:
			c.emit(taivm.OpSub)
		case syntax.STAR:
			c.emit(taivm.OpMul)
		case syntax.SLASH:
			c.emit(taivm.OpDiv)
		case syntax.PERCENT:
			c.emit(taivm.OpMod)
		case syntax.EQL:
			c.emit(taivm.OpEq)
		case syntax.NEQ:
			c.emit(taivm.OpNe)
		case syntax.LT:
			c.emit(taivm.OpLt)
		case syntax.LE:
			c.emit(taivm.OpLe)
		case syntax.GT:
			c.emit(taivm.OpGt)
		case syntax.GE:
			c.emit(taivm.OpGe)
		case syntax.PIPE:
			c.emit(taivm.OpBitOr)
		case syntax.AMP:
			c.emit(taivm.OpBitAnd)
		case syntax.CIRCUMFLEX:
			c.emit(taivm.OpBitXor)
		case syntax.LTLT:
			c.emit(taivm.OpBitLsh)
		case syntax.GTGT:
			c.emit(taivm.OpBitRsh)
		default:
			return fmt.Errorf("unsupported binary op: %v", e.Op)
		}
	case *syntax.CallExpr:
		if err := c.compileExpr(e.Fn); err != nil {
			return err
		}

		hasKw := false
		for _, arg := range e.Args {
			if bin, ok := arg.(*syntax.BinaryExpr); ok && bin.Op == syntax.EQ {
				hasKw = true
				break
			}
		}

		if !hasKw {
			for _, arg := range e.Args {
				if unary, ok := arg.(*syntax.UnaryExpr); ok && (unary.Op == syntax.STAR || unary.Op == syntax.STARSTAR) {
					return fmt.Errorf("star arguments not supported yet")
				}
				if err := c.compileExpr(arg); err != nil {
					return err
				}
			}
			c.emit(taivm.OpCall.With(len(e.Args)))
		} else {
			c.emit(taivm.OpLoadVar.With(c.addConst("__apply_kw")))
			c.emit(taivm.OpSwap)

			var posArgs []syntax.Expr
			var kwArgs []*syntax.BinaryExpr

			for _, arg := range e.Args {
				if bin, ok := arg.(*syntax.BinaryExpr); ok && bin.Op == syntax.EQ {
					kwArgs = append(kwArgs, bin)
				} else {
					if len(kwArgs) > 0 {
						return fmt.Errorf("positional argument follows keyword argument")
					}
					posArgs = append(posArgs, arg)
				}
			}

			for _, arg := range posArgs {
				if err := c.compileExpr(arg); err != nil {
					return err
				}
			}
			c.emit(taivm.OpMakeList.With(len(posArgs)))

			for _, kw := range kwArgs {
				id, ok := kw.X.(*syntax.Ident)
				if !ok {
					return fmt.Errorf("keyword argument must be identifier")
				}
				c.emit(taivm.OpLoadConst.With(c.addConst(id.Name)))
				if err := c.compileExpr(kw.Y); err != nil {
					return err
				}
			}
			c.emit(taivm.OpMakeMap.With(len(kwArgs)))

			c.emit(taivm.OpCall.With(3))
		}

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
	case *syntax.TupleExpr:
		for _, elem := range e.List {
			if err := c.compileExpr(elem); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeTuple.With(len(e.List)))
	case *syntax.ParenExpr:
		return c.compileExpr(e.X)
	case *syntax.SliceExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		if e.Lo != nil {
			if err := c.compileExpr(e.Lo); err != nil {
				return err
			}
		} else {
			c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
		}
		if e.Hi != nil {
			if err := c.compileExpr(e.Hi); err != nil {
				return err
			}
		} else {
			c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
		}
		if e.Step != nil {
			if err := c.compileExpr(e.Step); err != nil {
				return err
			}
		} else {
			c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
		}
		c.emit(taivm.OpGetSlice)

	case *syntax.DotExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		c.emit(taivm.OpLoadConst.With(c.addConst(e.Name.Name)))
		c.emit(taivm.OpGetAttr)

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
	loop := &loopContext{
		continueIP: startIP,
	}
	c.loops = append(c.loops, loop)

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

	for _, ip := range loop.breakIPs {
		c.patchJump(ip, c.currentIP())
	}
	c.loops = c.loops[:len(c.loops)-1]

	return nil
}

func (c *compiler) compileFor(s *syntax.ForStmt) error {
	if err := c.compileExpr(s.X); err != nil {
		return err
	}
	c.emit(taivm.OpGetIter)

	loopHeadIP := c.currentIP()
	loop := &loopContext{
		continueIP: loopHeadIP,
	}
	c.loops = append(c.loops, loop)

	// Emit NextIter with placeholder jump
	nextIterIP := c.currentIP()
	c.emit(taivm.OpNextIter)

	if err := c.compileStore(s.Vars); err != nil {
		return err
	}

	if err := c.compileStmts(s.Body); err != nil {
		return err
	}

	// Jump back to NextIter
	jumpBackIP := c.currentIP()
	c.emit(taivm.OpJump)
	c.patchJump(jumpBackIP, loopHeadIP)

	// Patch NextIter to jump to here (end of loop)
	endIP := c.currentIP()
	c.patchJump(nextIterIP, endIP)

	// Patch breaks
	for _, ip := range loop.breakIPs {
		c.patchJump(ip, endIP)
	}

	c.loops = c.loops[:len(c.loops)-1]
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
