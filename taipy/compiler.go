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
	case *syntax.LoadStmt:
		return c.compileLoad(s)
	default:
		return fmt.Errorf("unsupported statement type: %T", stmt)
	}
	return nil
}

func (c *compiler) compileLoad(s *syntax.LoadStmt) error {
	moduleName := s.ModuleName()
	c.emit(taivm.OpImport.With(c.addConst(moduleName)))

	for i, from := range s.From {
		to := s.To[i]
		c.emit(taivm.OpDup)
		c.emit(taivm.OpLoadConst.With(c.addConst(from.Name)))
		c.emit(taivm.OpGetAttr)
		c.emit(taivm.OpDefVar.With(c.addConst(to.Name)))
	}
	c.emit(taivm.OpPop)
	return nil
}

func (c *compiler) compileAssign(s *syntax.AssignStmt) error {
	if s.Op == syntax.EQ {
		return c.compileSimpleAssign(s.LHS, s.RHS)
	}
	return c.compileAugmentedAssign(s)
}

func (c *compiler) compileStore(lhs syntax.Expr) error {
	switch node := lhs.(type) {
	case *syntax.Ident:
		c.emit(taivm.OpDefVar.With(c.addConst(node.Name)))
		return nil
	case *syntax.ParenExpr:
		return c.compileStore(node.X)
	case *syntax.ListExpr:
		c.emit(taivm.OpUnpack.With(len(node.List)))
		for _, elem := range node.List {
			if err := c.compileStore(elem); err != nil {
				return err
			}
		}
		return nil
	case *syntax.TupleExpr:
		c.emit(taivm.OpUnpack.With(len(node.List)))
		for _, elem := range node.List {
			if err := c.compileStore(elem); err != nil {
				return err
			}
		}
		return nil
	case *syntax.DotExpr:
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp")))
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		c.emit(taivm.OpLoadConst.With(c.addConst(node.Name.Name)))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp")))
		c.emit(taivm.OpSetAttr)
		return nil
	case *syntax.IndexExpr:
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp")))
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		if err := c.compileExpr(node.Y); err != nil {
			return err
		}
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp")))
		c.emit(taivm.OpSetIndex)
		return nil
	case *syntax.SliceExpr:
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp")))
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		if err := c.compileSliceArgs(node); err != nil {
			return err
		}
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp")))
		c.emit(taivm.OpSetSlice)
		return nil
	default:
		return fmt.Errorf("unsupported variable type: %T", lhs)
	}
}

func (c *compiler) compileBranch(s *syntax.BranchStmt) error {
	if s.Token == syntax.PASS {
		return nil
	}

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
		return c.compileUnaryExpr(e)
	case *syntax.BinaryExpr:
		return c.compileBinaryExpr(e)
	case *syntax.CallExpr:
		return c.compileCallExpr(e)
	case *syntax.ListExpr:
		return c.compileListExpr(e)
	case *syntax.DictExpr:
		return c.compileDictExpr(e)
	case *syntax.IndexExpr:
		return c.compileIndexExpr(e)
	case *syntax.TupleExpr:
		return c.compileTupleExpr(e)
	case *syntax.ParenExpr:
		return c.compileExpr(e.X)
	case *syntax.SliceExpr:
		return c.compileSliceExpr(e)
	case *syntax.DotExpr:
		return c.compileDotExpr(e)
	case *syntax.CondExpr:
		return c.compileCondExpr(e)
	case *syntax.LambdaExpr:
		return c.compileLambdaExpr(e)
	case *syntax.Comprehension:
		return c.compileComprehension(e)
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

	// Handle breaks: they jump here, need to pop iterator
	if len(loop.breakIPs) > 0 {
		breakIP := c.currentIP()
		c.emit(taivm.OpPop)
		for _, ip := range loop.breakIPs {
			c.patchJump(ip, breakIP)
		}
	}

	// Patch NextIter to jump to here (end of loop)
	endIP := c.currentIP()
	c.patchJump(nextIterIP, endIP)

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
	var err error
	var isVariadic bool
	var defaults []syntax.Expr
	fn.ParamNames, defaults, isVariadic, err = c.extractParamNames(s.Params)
	if err != nil {
		return err
	}
	fn.NumParams = len(fn.ParamNames)
	fn.NumDefaults = len(defaults)
	fn.Variadic = isVariadic

	for _, d := range defaults {
		if err := c.compileExpr(d); err != nil {
			return err
		}
	}

	c.emit(taivm.OpMakeClosure.With(c.addConst(fn)))
	c.emit(taivm.OpDefVar.With(c.addConst(s.Name.Name)))

	return nil
}

func (c *compiler) extractParamNames(params []syntax.Expr) ([]string, []syntax.Expr, bool, error) {
	names := make([]string, 0, len(params))
	var defaults []syntax.Expr
	isVariadic := false
	seenDefault := false

	for _, p := range params {
		if isVariadic {
			return nil, nil, false, fmt.Errorf("variadic parameter must be last")
		}
		if id, ok := p.(*syntax.Ident); ok {
			if seenDefault {
				return nil, nil, false, fmt.Errorf("non-default argument follows default argument")
			}
			names = append(names, id.Name)
		} else if u, ok := p.(*syntax.UnaryExpr); ok && u.Op == syntax.STAR {
			if id, ok := u.X.(*syntax.Ident); ok {
				names = append(names, id.Name)
				isVariadic = true
			} else {
				return nil, nil, false, fmt.Errorf("variadic parameter must be identifier")
			}
		} else if bin, ok := p.(*syntax.BinaryExpr); ok && bin.Op == syntax.EQ {
			if id, ok := bin.X.(*syntax.Ident); ok {
				names = append(names, id.Name)
				defaults = append(defaults, bin.Y)
				seenDefault = true
			} else {
				return nil, nil, false, fmt.Errorf("parameter name must be identifier")
			}
		} else {
			return nil, nil, false, fmt.Errorf("complex parameters not supported")
		}
	}
	return names, defaults, isVariadic, nil
}

func (c *compiler) compileSimpleAssign(lhs, rhs syntax.Expr) error {
	switch node := lhs.(type) {
	case *syntax.Ident, *syntax.ListExpr, *syntax.TupleExpr, *syntax.ParenExpr:
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		return c.compileStore(node)
	case *syntax.IndexExpr:
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		if err := c.compileExpr(node.Y); err != nil {
			return err
		}
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpSetIndex)
	case *syntax.SliceExpr:
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		if err := c.compileSliceArgs(node); err != nil {
			return err
		}
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpSetSlice)
	case *syntax.DotExpr:
		if err := c.compileExpr(node.X); err != nil {
			return err
		}
		c.emit(taivm.OpLoadConst.With(c.addConst(node.Name.Name)))
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpSetAttr)
	default:
		return fmt.Errorf("unsupported assignment target: %T", lhs)
	}
	return nil
}

func (c *compiler) compileAugmentedAssign(s *syntax.AssignStmt) error {
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
	case syntax.SLASHSLASH_EQ:
		op = taivm.OpFloorDiv
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
		c.emit(taivm.OpDup2)
		c.emit(taivm.OpGetAttr)
		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(op)
		c.emit(taivm.OpSetAttr)

	case *syntax.SliceExpr:
		if err := c.compileExpr(lhs.X); err != nil {
			return err
		}
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp_target")))

		if err := c.compileSliceArgs(lhs); err != nil {
			return err
		}
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp_step")))
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp_hi")))
		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp_lo")))

		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_target")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_lo")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_hi")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_step")))
		c.emit(taivm.OpGetSlice)

		if err := c.compileExpr(s.RHS); err != nil {
			return err
		}
		c.emit(op)

		c.emit(taivm.OpDefVar.With(c.addConst(".$tmp_res")))

		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_target")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_lo")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_hi")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_step")))
		c.emit(taivm.OpLoadVar.With(c.addConst(".$tmp_res")))
		c.emit(taivm.OpSetSlice)

	default:
		return fmt.Errorf("unsupported augmented assignment target: %T", s.LHS)
	}
	return nil
}

func (c *compiler) compileUnaryExpr(e *syntax.UnaryExpr) error {
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
	return nil
}

func (c *compiler) compileBinaryExpr(e *syntax.BinaryExpr) error {
	// Handle short-circuit operators
	if e.Op == syntax.AND {
		// x and y
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		c.emit(taivm.OpDup)
		jumpFalseIP := c.currentIP()
		c.emit(taivm.OpJumpFalse)
		c.emit(taivm.OpPop)
		if err := c.compileExpr(e.Y); err != nil {
			return err
		}
		c.patchJump(jumpFalseIP, c.currentIP())
		return nil
	}
	if e.Op == syntax.OR {
		// x or y
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		c.emit(taivm.OpDup)
		jumpFalseIP := c.currentIP()
		c.emit(taivm.OpJumpFalse)

		// X is true, jump to end
		jumpEndIP := c.currentIP()
		c.emit(taivm.OpJump)

		// X is false
		c.patchJump(jumpFalseIP, c.currentIP())
		c.emit(taivm.OpPop)
		if err := c.compileExpr(e.Y); err != nil {
			return err
		}

		c.patchJump(jumpEndIP, c.currentIP())
		return nil
	}

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
	case syntax.SLASHSLASH:
		c.emit(taivm.OpFloorDiv)
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
	case syntax.IN:
		c.emit(taivm.OpContains)
	case syntax.NOT_IN:
		c.emit(taivm.OpContains)
		c.emit(taivm.OpNot)
	case syntax.STARSTAR:
		c.emit(taivm.OpPow)
	default:
		return fmt.Errorf("unsupported binary op: %v", e.Op)
	}
	return nil
}

func (c *compiler) compileCallExpr(e *syntax.CallExpr) error {
	// Optimize pow(a, b) -> OpPow
	if ident, ok := e.Fn.(*syntax.Ident); ok && ident.Name == "pow" && len(e.Args) == 2 {
		simple := true
		for _, arg := range e.Args {
			if _, ok := arg.(*syntax.BinaryExpr); ok {
				simple = false
				break
			}
			if u, ok := arg.(*syntax.UnaryExpr); ok && (u.Op == syntax.STAR || u.Op == syntax.STARSTAR) {
				simple = false
				break
			}
		}
		if simple {
			if err := c.compileExpr(e.Args[0]); err != nil {
				return err
			}
			if err := c.compileExpr(e.Args[1]); err != nil {
				return err
			}
			c.emit(taivm.OpPow)
			return nil
		}
	}

	if err := c.compileExpr(e.Fn); err != nil {
		return err
	}

	isSimple := true
	for _, arg := range e.Args {
		if _, ok := arg.(*syntax.BinaryExpr); ok {
			isSimple = false
			break
		}
		if u, ok := arg.(*syntax.UnaryExpr); ok && (u.Op == syntax.STAR || u.Op == syntax.STARSTAR) {
			isSimple = false
			break
		}
	}

	if isSimple {
		for _, arg := range e.Args {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(taivm.OpCall.With(len(e.Args)))
		return nil
	}

	// Dynamic path using OpCallKw (callee is already on stack)

	// 1. Build Positional Args List
	hasListOnStack := false
	var pendingPos []syntax.Expr

	flushPos := func() error {
		if len(pendingPos) == 0 && hasListOnStack {
			return nil
		}
		for _, arg := range pendingPos {
			if err := c.compileExpr(arg); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(pendingPos)))
		if hasListOnStack {
			c.emit(taivm.OpAdd)
		}
		hasListOnStack = true
		pendingPos = nil
		return nil
	}

	for _, arg := range e.Args {
		if bin, ok := arg.(*syntax.BinaryExpr); ok && bin.Op == syntax.EQ {
			continue
		}
		if u, ok := arg.(*syntax.UnaryExpr); ok && u.Op == syntax.STARSTAR {
			continue
		}

		if u, ok := arg.(*syntax.UnaryExpr); ok && u.Op == syntax.STAR {
			if err := flushPos(); err != nil {
				return err
			}
			if err := c.compileExpr(u.X); err != nil {
				return err
			}
			if hasListOnStack {
				c.emit(taivm.OpAdd)
			} else {
				hasListOnStack = true
			}
		} else {
			pendingPos = append(pendingPos, arg)
		}
	}
	if err := flushPos(); err != nil {
		return err
	}
	if !hasListOnStack {
		c.emit(taivm.OpMakeList.With(0))
	}

	// 2. Build Keyword Args Map
	hasMapOnStack := false
	var pendingKw []*syntax.BinaryExpr

	flushKw := func() error {
		if len(pendingKw) == 0 && hasMapOnStack {
			return nil
		}
		for _, kw := range pendingKw {
			id := kw.X.(*syntax.Ident)
			c.emit(taivm.OpLoadConst.With(c.addConst(id.Name)))
			if err := c.compileExpr(kw.Y); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeMap.With(len(pendingKw)))
		if hasMapOnStack {
			c.emit(taivm.OpBitOr)
		}
		hasMapOnStack = true
		pendingKw = nil
		return nil
	}

	for _, arg := range e.Args {
		if bin, ok := arg.(*syntax.BinaryExpr); ok && bin.Op == syntax.EQ {
			pendingKw = append(pendingKw, bin)
		} else if u, ok := arg.(*syntax.UnaryExpr); ok && u.Op == syntax.STARSTAR {
			if err := flushKw(); err != nil {
				return err
			}
			if err := c.compileExpr(u.X); err != nil {
				return err
			}
			if hasMapOnStack {
				c.emit(taivm.OpBitOr)
			} else {
				hasMapOnStack = true
			}
		}
	}
	if err := flushKw(); err != nil {
		return err
	}
	if !hasMapOnStack {
		c.emit(taivm.OpMakeMap.With(0))
	}

	c.emit(taivm.OpCallKw)
	return nil
}

func (c *compiler) compileListExpr(e *syntax.ListExpr) error {
	for _, elem := range e.List {
		if err := c.compileExpr(elem); err != nil {
			return err
		}
	}
	c.emit(taivm.OpMakeList.With(len(e.List)))
	return nil
}

func (c *compiler) compileDictExpr(e *syntax.DictExpr) error {
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
	return nil
}

func (c *compiler) compileIndexExpr(e *syntax.IndexExpr) error {
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	if err := c.compileExpr(e.Y); err != nil {
		return err
	}
	c.emit(taivm.OpGetIndex)
	return nil
}

func (c *compiler) compileTupleExpr(e *syntax.TupleExpr) error {
	for _, elem := range e.List {
		if err := c.compileExpr(elem); err != nil {
			return err
		}
	}
	c.emit(taivm.OpMakeTuple.With(len(e.List)))
	return nil
}

func (c *compiler) compileSliceArgs(node *syntax.SliceExpr) error {
	if node.Lo != nil {
		if err := c.compileExpr(node.Lo); err != nil {
			return err
		}
	} else {
		c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
	}
	if node.Hi != nil {
		if err := c.compileExpr(node.Hi); err != nil {
			return err
		}
	} else {
		c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
	}
	if node.Step != nil {
		if err := c.compileExpr(node.Step); err != nil {
			return err
		}
	} else {
		c.emit(taivm.OpLoadConst.With(c.addConst(nil)))
	}
	return nil
}

func (c *compiler) compileSliceExpr(e *syntax.SliceExpr) error {
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	if err := c.compileSliceArgs(e); err != nil {
		return err
	}
	c.emit(taivm.OpGetSlice)
	return nil
}

func (c *compiler) compileDotExpr(e *syntax.DotExpr) error {
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	c.emit(taivm.OpLoadConst.With(c.addConst(e.Name.Name)))
	c.emit(taivm.OpGetAttr)
	return nil
}

func (c *compiler) compileCondExpr(e *syntax.CondExpr) error {
	if err := c.compileExpr(e.Cond); err != nil {
		return err
	}
	jumpFalseIP := c.currentIP()
	c.emit(taivm.OpJumpFalse)

	if err := c.compileExpr(e.True); err != nil {
		return err
	}

	jumpEndIP := c.currentIP()
	c.emit(taivm.OpJump)

	c.patchJump(jumpFalseIP, c.currentIP())

	if err := c.compileExpr(e.False); err != nil {
		return err
	}

	c.patchJump(jumpEndIP, c.currentIP())
	return nil
}

func (c *compiler) compileLambdaExpr(e *syntax.LambdaExpr) error {
	sub := newCompiler("<lambda>")
	if err := sub.compileExpr(e.Body); err != nil {
		return err
	}
	sub.emit(taivm.OpReturn)

	fn := sub.toFunction()
	var err error
	var isVariadic bool
	var defaults []syntax.Expr
	fn.ParamNames, defaults, isVariadic, err = c.extractParamNames(e.Params)
	if err != nil {
		return err
	}
	fn.NumParams = len(fn.ParamNames)
	fn.NumDefaults = len(defaults)
	fn.Variadic = isVariadic

	for _, d := range defaults {
		if err := c.compileExpr(d); err != nil {
			return err
		}
	}

	c.emit(taivm.OpMakeClosure.With(c.addConst(fn)))
	return nil
}

func (c *compiler) compileComprehension(e *syntax.Comprehension) error {
	c.emit(taivm.OpEnterScope)

	if e.Curly {
		c.emit(taivm.OpMakeMap.With(0))
	} else {
		c.emit(taivm.OpMakeList.With(0))
	}

	resultName := ".result"
	c.emit(taivm.OpDefVar.With(c.addConst(resultName)))

	if err := c.compileComprehensionClauses(e, 0, resultName); err != nil {
		return err
	}

	c.emit(taivm.OpLoadVar.With(c.addConst(resultName)))
	c.emit(taivm.OpLeaveScope)
	return nil
}

func (c *compiler) compileComprehensionClauses(e *syntax.Comprehension, idx int, resultName string) error {
	if idx >= len(e.Clauses) {
		// Base case: emit body
		if e.Curly {
			// Dict comprehension: {Key: Value}
			entry, ok := e.Body.(*syntax.DictEntry)
			if !ok {
				return fmt.Errorf("dict comprehension body must be DictEntry")
			}

			c.emit(taivm.OpLoadVar.With(c.addConst(resultName)))
			if err := c.compileExpr(entry.Key); err != nil {
				return err
			}
			if err := c.compileExpr(entry.Value); err != nil {
				return err
			}
			c.emit(taivm.OpSetIndex)
		} else {
			// List comprehension: [Body]
			c.emit(taivm.OpLoadVar.With(c.addConst(resultName)))
			if err := c.compileExpr(e.Body); err != nil {
				return err
			}
			c.emit(taivm.OpListAppend)
			c.emit(taivm.OpPop)
		}
		return nil
	}

	clause := e.Clauses[idx]
	switch cl := clause.(type) {
	case *syntax.ForClause:
		if err := c.compileExpr(cl.X); err != nil {
			return err
		}
		c.emit(taivm.OpGetIter)

		loopHeadIP := c.currentIP()
		nextIterIP := c.currentIP()
		c.emit(taivm.OpNextIter)

		if err := c.compileStore(cl.Vars); err != nil {
			return err
		}

		if err := c.compileComprehensionClauses(e, idx+1, resultName); err != nil {
			return err
		}

		// Jump back
		jumpBackIP := c.currentIP()
		c.emit(taivm.OpJump)
		c.patchJump(jumpBackIP, loopHeadIP)

		// Patch NextIter (exit loop)
		endIP := c.currentIP()
		c.patchJump(nextIterIP, endIP)

	case *syntax.IfClause:
		if err := c.compileExpr(cl.Cond); err != nil {
			return err
		}
		jumpFalseIP := c.currentIP()
		c.emit(taivm.OpJumpFalse)

		if err := c.compileComprehensionClauses(e, idx+1, resultName); err != nil {
			return err
		}

		c.patchJump(jumpFalseIP, c.currentIP())

	default:
		return fmt.Errorf("unsupported comprehension clause: %T", clause)
	}

	return nil
}
