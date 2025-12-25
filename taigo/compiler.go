package taigo

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"github.com/reusee/tai/taivm"
)

type compiler struct {
	name       string
	code       []taivm.OpCode
	constants  []any
	loops      []*loopScope
	scopeDepth int
}

type loopScope struct {
	breakTargets    []int
	continueTargets []int
	startPos        int
	postPos         int // position of post statement if any
	entryDepth      int
	isRange         bool
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

func (c *compiler) emitJump(op taivm.OpCode) int {
	idx := len(c.code)
	c.emit(op)
	return idx
}

func (c *compiler) patchJump(idx int, target int) {
	// The immediate offset in OpJump is added to IP *after* IP is incremented.
	// target = (idx + 1) + offset
	// offset = target - idx - 1
	offset := target - idx - 1
	inst := c.code[idx]
	// Preserve opcode, set offset in upper 24 bits
	op := inst & 0xff
	c.code[idx] = op | (taivm.OpCode(offset) << 8)
}

func (c *compiler) enterLoop() *loopScope {
	scope := &loopScope{
		startPos:   len(c.code),
		entryDepth: c.scopeDepth,
	}
	c.loops = append(c.loops, scope)
	return scope
}

func (c *compiler) leaveLoop() {
	if len(c.loops) == 0 {
		return
	}
	scope := c.loops[len(c.loops)-1]
	c.loops = c.loops[:len(c.loops)-1]
	end := len(c.code)
	for _, idx := range scope.breakTargets {
		c.patchJump(idx, end)
	}
	// loops with post statement handle their own continue targets differently
	// but simple loops jump to start
	target := scope.startPos
	if scope.postPos > 0 {
		target = scope.postPos
	}
	for _, idx := range scope.continueTargets {
		c.patchJump(idx, target)
	}
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
	var initDecls []*ast.FuncDecl
	var hasMain bool

	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Recv == nil && funcDecl.Name.Name == "init" {
				initDecls = append(initDecls, funcDecl)
				continue
			}
			if funcDecl.Name.Name == "main" {
				hasMain = true
			}
		}

		if err := c.compileDecl(decl); err != nil {
			return err
		}
	}

	for _, decl := range initDecls {
		if err := c.compileInitFunc(decl); err != nil {
			return err
		}
	}

	if hasMain {
		idx := c.addConst("main")
		c.emit(taivm.OpLoadVar.With(idx))
		c.emit(taivm.OpCall.With(0))
		c.emit(taivm.OpPop)
	}

	return nil
}

func (c *compiler) compileDecl(decl ast.Decl) error {
	switch d := decl.(type) {

	case *ast.FuncDecl:
		return c.compileFuncDecl(d)

	case *ast.GenDecl:
		if d.Tok == token.VAR || d.Tok == token.CONST || d.Tok == token.IMPORT {
			return c.compileGenDecl(d)
		}
		if d.Tok == token.TYPE {
			return nil
		}

	default:
		return fmt.Errorf("unknown declaration type: %T", decl)

	}

	return nil
}

func (c *compiler) compileFuncDecl(decl *ast.FuncDecl) error {
	fn, err := c.compileFunc(decl)
	if err != nil {
		return err
	}

	idx := c.addConst(fn)
	c.emit(taivm.OpMakeClosure.With(idx))

	nameIdx := c.addConst(decl.Name.Name)
	c.emit(taivm.OpDefVar.With(nameIdx))

	return nil
}

func (c *compiler) compileInitFunc(decl *ast.FuncDecl) error {
	fn, err := c.compileFunc(decl)
	if err != nil {
		return err
	}

	idx := c.addConst(fn)
	c.emit(taivm.OpMakeClosure.With(idx))
	c.emit(taivm.OpCall.With(0))
	c.emit(taivm.OpPop)

	return nil
}

func (c *compiler) compileFunc(decl *ast.FuncDecl) (*taivm.Function, error) {
	sub := &compiler{
		name: decl.Name.Name,
	}

	for _, stmt := range decl.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return nil, err
		}
	}

	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)

	fn := sub.getFunction()
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				fn.Variadic = true
			}
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

	return fn, nil
}

func (c *compiler) compileExpr(expr ast.Expr) error {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return c.compileBasicLiteral(e)

	case *ast.Ident:
		return c.compileIdentifier(e)

	case *ast.BinaryExpr:
		return c.compileBinaryExpr(e)

	case *ast.UnaryExpr:
		return c.compileUnaryExpr(e)

	case *ast.CallExpr:
		return c.compileCallExpr(e)

	case *ast.ParenExpr:
		return c.compileExpr(e.X)

	case *ast.IndexExpr:
		return c.compileIndexExpr(e)

	case *ast.SelectorExpr:
		return c.compileSelectorExpr(e)

	case *ast.SliceExpr:
		return c.compileSliceExpr(e)

	case *ast.FuncLit:
		return c.compileFuncLit(e)

	case *ast.CompositeLit:
		return c.compileCompositeLit(e)

	case *ast.StarExpr:
		// taivm is dynamic; treat pointer dereference as identity
		return c.compileExpr(e.X)

	case *ast.TypeAssertExpr:
		// taivm is dynamic; treat assertion as pass-through
		return c.compileExpr(e.X)

	case *ast.KeyValueExpr:
		return fmt.Errorf("key:value expression not supported outside composite literal")

	case *ast.Ellipsis:
		return fmt.Errorf("ellipsis expression not supported outside call")

	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		s, err := c.typeToString(e)
		if err != nil {
			return err
		}
		c.loadConst(s)
		return nil

	case *ast.BadExpr:
		return fmt.Errorf("bad expression")

	default:
		return fmt.Errorf("unknown expr type: %T", expr)
	}
}

func (c *compiler) emitBinaryOp(tok token.Token) error {
	if tok == token.AND_NOT || tok == token.AND_NOT_ASSIGN {
		c.emit(taivm.OpBitNot)
		c.emit(taivm.OpBitAnd)
		return nil
	}
	op, ok := c.tokenToOp(tok)
	if !ok {
		return fmt.Errorf("unknown operator: %s", tok)
	}
	c.emit(op)
	return nil
}

func (c *compiler) tokenToOp(tok token.Token) (taivm.OpCode, bool) {
	switch tok {
	case token.ADD, token.ADD_ASSIGN:
		return taivm.OpAdd, true
	case token.SUB, token.SUB_ASSIGN:
		return taivm.OpSub, true
	case token.MUL, token.MUL_ASSIGN:
		return taivm.OpMul, true
	case token.QUO, token.QUO_ASSIGN:
		return taivm.OpDiv, true
	case token.REM, token.REM_ASSIGN:
		return taivm.OpMod, true
	case token.EQL:
		return taivm.OpEq, true
	case token.NEQ:
		return taivm.OpNe, true
	case token.LSS:
		return taivm.OpLt, true
	case token.LEQ:
		return taivm.OpLe, true
	case token.GTR:
		return taivm.OpGt, true
	case token.GEQ:
		return taivm.OpGe, true
	case token.AND, token.AND_ASSIGN:
		return taivm.OpBitAnd, true
	case token.OR, token.OR_ASSIGN:
		return taivm.OpBitOr, true
	case token.XOR, token.XOR_ASSIGN:
		return taivm.OpBitXor, true
	case token.SHL, token.SHL_ASSIGN:
		return taivm.OpBitLsh, true
	case token.SHR, token.SHR_ASSIGN:
		return taivm.OpBitRsh, true
	case token.NOT:
		return taivm.OpNot, true
	}
	return 0, false
}

func (c *compiler) compileBasicLiteral(expr *ast.BasicLit) error {
	switch expr.Kind {

	case token.INT:
		v, err := strconv.ParseInt(expr.Value, 0, 64)
		if err != nil {
			return err
		}
		c.loadConst(v)

	case token.FLOAT:
		v, err := strconv.ParseFloat(expr.Value, 64)
		if err != nil {
			return err
		}
		c.loadConst(v)

	case token.STRING:
		v, err := strconv.Unquote(expr.Value)
		if err != nil {
			return err
		}
		c.loadConst(v)

	case token.CHAR:
		v, _, _, err := strconv.UnquoteChar(expr.Value, '\'')
		if err != nil {
			return err
		}
		c.loadConst(int64(v))

	default:
		return fmt.Errorf("unknown basic lit kind: %v", expr.Kind)
	}

	return nil
}

func (c *compiler) compileIdentifier(expr *ast.Ident) error {
	switch expr.Name {
	case "true":
		c.loadConst(true)
	case "false":
		c.loadConst(false)
	case "nil":
		c.loadConst(nil)
	default:
		idx := c.addConst(expr.Name)
		c.emit(taivm.OpLoadVar.With(idx))
	}
	return nil
}

func (c *compiler) compileBinaryExpr(expr *ast.BinaryExpr) error {
	if expr.Op == token.LAND {
		return c.compileLogicAnd(expr)
	}
	if expr.Op == token.LOR {
		return c.compileLogicOr(expr)
	}

	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	if err := c.compileExpr(expr.Y); err != nil {
		return err
	}

	return c.emitBinaryOp(expr.Op)
}

func (c *compiler) compileLogicAnd(expr *ast.BinaryExpr) error {
	// a && b
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	// Stack: [a]
	c.emit(taivm.OpDup)
	// Stack: [a, a]
	jumpEnd := c.emitJump(taivm.OpJumpFalse)

	// Fallthrough: a is true. Result is result of b.
	c.emit(taivm.OpPop) // Pop a
	if err := c.compileExpr(expr.Y); err != nil {
		return err
	}

	c.patchJump(jumpEnd, len(c.code))
	return nil
}

func (c *compiler) compileLogicOr(expr *ast.BinaryExpr) error {
	// a || b
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	// Stack: [a]
	c.emit(taivm.OpDup)
	// Stack: [a, a]
	// If a is false, jump to eval b
	jumpEvalB := c.emitJump(taivm.OpJumpFalse)

	// Fallthrough: a is true. Result is a.
	jumpEnd := c.emitJump(taivm.OpJump)

	// Eval b
	c.patchJump(jumpEvalB, len(c.code))
	c.emit(taivm.OpPop) // Pop a (which was false)
	if err := c.compileExpr(expr.Y); err != nil {
		return err
	}

	c.patchJump(jumpEnd, len(c.code))
	return nil
}

func (c *compiler) compileUnaryExpr(expr *ast.UnaryExpr) error {
	switch expr.Op {

	case token.SUB:
		c.loadConst(0)
		if err := c.compileExpr(expr.X); err != nil {
			return err
		}
		c.emit(taivm.OpSub)

	case token.ADD:
		if err := c.compileExpr(expr.X); err != nil {
			return err
		}

	case token.AND:
		return fmt.Errorf("address-of operator & not supported")

	default:
		if err := c.compileExpr(expr.X); err != nil {
			return err
		}
		switch expr.Op {

		case token.NOT:
			c.emit(taivm.OpNot)

		case token.XOR:
			c.emit(taivm.OpBitNot)

		default:
			return fmt.Errorf("unknown unary operator: %s", expr.Op)

		}
	}

	return nil
}

func (c *compiler) compileCallExpr(expr *ast.CallExpr) error {
	if err := c.compileExpr(expr.Fun); err != nil {
		return err
	}

	if expr.Ellipsis != token.NoPos {
		// Variadic expansion: f(x, y, z...)
		// Compile explicit args into a list
		numExplicit := len(expr.Args) - 1
		for i := range numExplicit {
			if err := c.compileExpr(expr.Args[i]); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(numExplicit))

		// Compile spread argument (must be slice/list)
		if err := c.compileExpr(expr.Args[numExplicit]); err != nil {
			return err
		}

		// Concatenate lists: [x, y] + z
		c.emit(taivm.OpAdd)

		// Empty kwargs map
		c.emit(taivm.OpMakeMap.With(0))

		// Call with args list and kw map
		c.emit(taivm.OpCallKw)
		return nil
	}

	for _, arg := range expr.Args {
		if err := c.compileExpr(arg); err != nil {
			return err
		}
	}
	c.emit(taivm.OpCall.With(len(expr.Args)))
	return nil
}

func (c *compiler) compileIndexExpr(expr *ast.IndexExpr) error {
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	if err := c.compileExpr(expr.Index); err != nil {
		return err
	}
	c.emit(taivm.OpGetIndex)
	return nil
}

func (c *compiler) compileSelectorExpr(expr *ast.SelectorExpr) error {
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	c.loadConst(expr.Sel.Name)
	c.emit(taivm.OpGetAttr)
	return nil
}

func (c *compiler) compileSliceExpr(expr *ast.SliceExpr) error {
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	// Low
	if expr.Low != nil {
		if err := c.compileExpr(expr.Low); err != nil {
			return err
		}
	} else {
		c.loadConst(nil)
	}
	// High
	if expr.High != nil {
		if err := c.compileExpr(expr.High); err != nil {
			return err
		}
	} else {
		c.loadConst(nil)
	}
	// Step (implicit 1 for simple slice, explicit not supported in Go syntax directly without 3-index slice which is cap)
	// taivm expects step. Pushing nil lets taivm default to 1
	c.loadConst(nil)

	c.emit(taivm.OpGetSlice)
	return nil
}

func (c *compiler) compileFuncLit(expr *ast.FuncLit) error {
	sub := &compiler{
		name: "anon",
	}

	for _, stmt := range expr.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return err
		}
	}

	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)

	fn := sub.getFunction()
	if expr.Type.Params != nil {
		for _, field := range expr.Type.Params.List {
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				fn.Variadic = true
			}
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
	return nil
}

func (c *compiler) compileCompositeLit(expr *ast.CompositeLit) error {
	switch expr.Type.(type) {
	case *ast.ArrayType:
		for _, elt := range expr.Elts {
			if err := c.compileExpr(elt); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(expr.Elts)))

	default:
		// Treat Structs and Maps similarly
		for _, elt := range expr.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				// field: value is required for maps/structs in this basic implementation
				return fmt.Errorf("element must be key:value")
			}
			// Key
			// Struct field names in Go AST are often Idents, but in map literals they are BasicLits
			// OpMakeMap expects keys on stack.
			if ident, ok := kv.Key.(*ast.Ident); ok {
				c.loadConst(ident.Name)
			} else {
				if err := c.compileExpr(kv.Key); err != nil {
					return err
				}
			}
			// Value
			if err := c.compileExpr(kv.Value); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeMap.With(len(expr.Elts)))
	}
	return nil
}

func (c *compiler) compileStmt(stmt ast.Stmt) error {
	switch s := stmt.(type) {

	case *ast.ExprStmt:
		return c.compileExprStmt(s)

	case *ast.BlockStmt:
		return c.compileBlockStmt(s)

	case *ast.ReturnStmt:
		return c.compileReturnStmt(s)

	case *ast.AssignStmt:
		return c.compileAssignStmt(s)

	case *ast.IncDecStmt:
		return c.compileIncDecStmt(s)

	case *ast.DeclStmt:
		return c.compileDeclStmt(s)

	case *ast.IfStmt:
		return c.compileIfStmt(s)

	case *ast.SwitchStmt:
		return c.compileSwitchStmt(s)

	case *ast.ForStmt:
		return c.compileForStmt(s)

	case *ast.RangeStmt:
		return c.compileRangeStmt(s)

	case *ast.BranchStmt:
		return c.compileBranchStmt(s)

	case *ast.EmptyStmt:
		return nil

	case *ast.LabeledStmt:
		return c.compileStmt(s.Stmt)

	case *ast.GoStmt:
		return fmt.Errorf("go statement not supported")

	case *ast.DeferStmt:
		return fmt.Errorf("defer statement not supported")

	case *ast.SelectStmt:
		return fmt.Errorf("select statement not supported")

	case *ast.SendStmt:
		return fmt.Errorf("send statement not supported")

	case *ast.TypeSwitchStmt:
		return fmt.Errorf("type switch statement not supported")

	case *ast.BadStmt:
		return fmt.Errorf("bad statement")

	default:
		return fmt.Errorf("unknown stmt type: %T", stmt)
	}
}

func (c *compiler) compileExprStmt(stmt *ast.ExprStmt) error {
	if err := c.compileExpr(stmt.X); err != nil {
		return err
	}
	c.emit(taivm.OpPop)
	return nil
}

func (c *compiler) compileBlockStmt(stmt *ast.BlockStmt) error {
	c.emit(taivm.OpEnterScope)
	c.scopeDepth++
	for _, stmt := range stmt.List {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	c.emit(taivm.OpLeaveScope)
	c.scopeDepth--
	return nil
}

func (c *compiler) compileReturnStmt(stmt *ast.ReturnStmt) error {
	if len(stmt.Results) == 0 {
		c.loadConst(nil)
		c.emit(taivm.OpReturn)

	} else if len(stmt.Results) == 1 {
		if err := c.compileExpr(stmt.Results[0]); err != nil {
			return err
		}
		c.emit(taivm.OpReturn)

	} else {
		for _, r := range stmt.Results {
			if err := c.compileExpr(r); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeTuple.With(len(stmt.Results)))
		c.emit(taivm.OpReturn)
	}

	return nil
}

func (c *compiler) compileAssignStmt(stmt *ast.AssignStmt) error {
	if len(stmt.Lhs) > 1 {
		return c.compileMultiAssign(stmt)
	}

	lhs := stmt.Lhs[0]
	rhs := stmt.Rhs[0]
	tok := stmt.Tok

	if tok == token.ASSIGN || tok == token.DEFINE {
		return c.compileSingleAssign(lhs, rhs, tok)
	}

	return c.compileCompoundAssign(lhs, rhs, tok)
}

func (c *compiler) compileMultiAssign(stmt *ast.AssignStmt) error {
	for _, lhs := range stmt.Lhs {
		if _, ok := lhs.(*ast.Ident); !ok {
			return fmt.Errorf("multi-assignment only supported for variables")
		}
	}

	if len(stmt.Rhs) == 1 {
		// a, b = f()
		if err := c.compileExpr(stmt.Rhs[0]); err != nil {
			return err
		}
		c.emit(taivm.OpUnpack.With(len(stmt.Lhs)))
		// OpUnpack puts 1st element at top, assign forward
		for i := 0; i < len(stmt.Lhs); i++ {
			ident := stmt.Lhs[i].(*ast.Ident)
			idx := c.addConst(ident.Name)
			if stmt.Tok == token.DEFINE {
				c.emit(taivm.OpDefVar.With(idx))
			} else {
				c.emit(taivm.OpSetVar.With(idx))
			}
		}

	} else if len(stmt.Rhs) == len(stmt.Lhs) {
		// a, b = 1, 2
		for _, r := range stmt.Rhs {
			if err := c.compileExpr(r); err != nil {
				return err
			}
		}
		// Stack: 1, 2 (Top). Assign reverse
		for i := len(stmt.Lhs) - 1; i >= 0; i-- {
			ident := stmt.Lhs[i].(*ast.Ident)
			idx := c.addConst(ident.Name)
			if stmt.Tok == token.DEFINE {
				c.emit(taivm.OpDefVar.With(idx))
			} else {
				c.emit(taivm.OpSetVar.With(idx))
			}
		}

	} else {
		return fmt.Errorf("assignment count mismatch: %d = %d", len(stmt.Lhs), len(stmt.Rhs))
	}

	return nil
}

func (c *compiler) compileSingleAssign(lhs, rhs ast.Expr, tok token.Token) error {
	// Index Assignment: a[i] = v
	if idxExpr, ok := lhs.(*ast.IndexExpr); ok {
		if err := c.compileExpr(idxExpr.X); err != nil {
			return err
		}
		if err := c.compileExpr(idxExpr.Index); err != nil {
			return err
		}
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpSetIndex)
		return nil
	}

	// Selector Assignment: a.f = v
	if selExpr, ok := lhs.(*ast.SelectorExpr); ok {
		if err := c.compileExpr(selExpr.X); err != nil {
			return err
		}
		c.loadConst(selExpr.Sel.Name)
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpSetAttr)
		return nil
	}

	// Variable Assignment: x = v
	if err := c.compileExpr(rhs); err != nil {
		return err
	}

	if ident, ok := lhs.(*ast.Ident); ok {
		idx := c.addConst(ident.Name)
		if tok == token.DEFINE {
			c.emit(taivm.OpDefVar.With(idx))
		} else {
			c.emit(taivm.OpSetVar.With(idx))
		}
		return nil
	}

	return fmt.Errorf("assignment to %T not supported", lhs)
}

func (c *compiler) compileCompoundAssign(lhs, rhs ast.Expr, tok token.Token) error {
	if idxExpr, ok := lhs.(*ast.IndexExpr); ok {
		// Target[Index] += Val
		if err := c.compileExpr(idxExpr.X); err != nil {
			return err
		}
		if err := c.compileExpr(idxExpr.Index); err != nil {
			return err
		}
		// Duplicate Target, Index
		c.emit(taivm.OpDup2)
		c.emit(taivm.OpGetIndex)

		// Eval RHS
		if err := c.compileExpr(rhs); err != nil {
			return err
		}

		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}

		c.emit(taivm.OpSetIndex)
		return nil
	}

	if selExpr, ok := lhs.(*ast.SelectorExpr); ok {
		// Target.Sel += Val
		if err := c.compileExpr(selExpr.X); err != nil {
			return err
		}
		c.emit(taivm.OpDup)
		c.loadConst(selExpr.Sel.Name)
		c.emit(taivm.OpGetAttr)

		if err := c.compileExpr(rhs); err != nil {
			return err
		}

		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}

		c.loadConst(selExpr.Sel.Name)
		c.emit(taivm.OpSwap)
		c.emit(taivm.OpSetAttr)
		return nil
	}

	if ident, ok := lhs.(*ast.Ident); ok {
		idx := c.addConst(ident.Name)
		c.emit(taivm.OpLoadVar.With(idx))
		if err := c.compileExpr(rhs); err != nil {
			return err
		}

		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}

		c.emit(taivm.OpSetVar.With(idx))
		return nil
	}

	return fmt.Errorf("compound assignment to %T not supported", lhs)
}

func (c *compiler) compileIncDecStmt(stmt *ast.IncDecStmt) error {
	fakeRhs := &ast.BasicLit{Kind: token.INT, Value: "1"}
	var tok token.Token
	if stmt.Tok == token.INC {
		tok = token.ADD_ASSIGN
	} else {
		tok = token.SUB_ASSIGN
	}

	// Synthesize an assignment statement: x += 1 or x -= 1
	assign := &ast.AssignStmt{
		Lhs: []ast.Expr{stmt.X},
		Tok: tok,
		Rhs: []ast.Expr{fakeRhs},
	}

	return c.compileAssignStmt(assign)
}

func (c *compiler) compileDeclStmt(stmt *ast.DeclStmt) error {
	decl, ok := stmt.Decl.(*ast.GenDecl)
	if !ok || (decl.Tok != token.VAR && decl.Tok != token.CONST) {
		return fmt.Errorf("only var/const decls supported in function body")
	}
	return c.compileGenDecl(decl)
}

func (c *compiler) compileIfStmt(stmt *ast.IfStmt) error {
	if stmt.Init != nil {
		c.emit(taivm.OpEnterScope)
		c.scopeDepth++
		if err := c.compileStmt(stmt.Init); err != nil {
			return err
		}
		defer func() {
			c.emit(taivm.OpLeaveScope)
			c.scopeDepth--
		}()
	}

	if err := c.compileExpr(stmt.Cond); err != nil {
		return err
	}

	jumpElse := c.emitJump(taivm.OpJumpFalse)

	if err := c.compileBlockStmt(stmt.Body); err != nil {
		return err
	}

	if stmt.Else != nil {
		jumpEnd := c.emitJump(taivm.OpJump)
		c.patchJump(jumpElse, len(c.code))

		if err := c.compileStmt(stmt.Else); err != nil {
			return err
		}
		c.patchJump(jumpEnd, len(c.code))
	} else {
		c.patchJump(jumpElse, len(c.code))
	}

	return nil
}

func (c *compiler) compileSwitchStmt(stmt *ast.SwitchStmt) error {
	c.emit(taivm.OpEnterScope)
	c.scopeDepth++
	defer func() {
		c.emit(taivm.OpLeaveScope)
		c.scopeDepth--
	}()

	if stmt.Init != nil {
		if err := c.compileStmt(stmt.Init); err != nil {
			return err
		}
	}

	if stmt.Tag != nil {
		if err := c.compileExpr(stmt.Tag); err != nil {
			return err
		}
	} else {
		c.loadConst(true)
	}

	var bodyJumps [][]int // case index -> list of jumps to it
	var defaultBodyIndex int = -1
	var endJumps []int

	// 1. Checks
	for i, clause := range stmt.Body.List {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			return fmt.Errorf("switch body must be case clause")
		}

		if len(cc.List) == 0 {
			defaultBodyIndex = i
			bodyJumps = append(bodyJumps, nil)
			continue
		}

		var jumps []int
		for _, expr := range cc.List {
			c.emit(taivm.OpDup)
			if err := c.compileExpr(expr); err != nil {
				return err
			}
			c.emit(taivm.OpEq)
			// If Equal, Jump Body
			skip := c.emitJump(taivm.OpJumpFalse)
			jumps = append(jumps, c.emitJump(taivm.OpJump))
			c.patchJump(skip, len(c.code))
		}
		bodyJumps = append(bodyJumps, jumps)
	}

	// If no match found, jump to default or end
	var defaultJump int
	if defaultBodyIndex != -1 {
		defaultJump = c.emitJump(taivm.OpJump)
	} else {
		endJumps = append(endJumps, c.emitJump(taivm.OpJump))
	}

	// 2. Bodies
	for i, clause := range stmt.Body.List {
		cc := clause.(*ast.CaseClause)

		target := len(c.code)
		if i == defaultBodyIndex {
			c.patchJump(defaultJump, target)
		} else {
			for _, jump := range bodyJumps[i] {
				c.patchJump(jump, target)
			}
		}

		if err := c.compileBlockStmt(&ast.BlockStmt{List: cc.Body}); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emitJump(taivm.OpJump))
	}

	endPos := len(c.code)
	for _, jump := range endJumps {
		c.patchJump(jump, endPos)
	}

	// Clean up tag
	c.emit(taivm.OpPop)

	return nil
}

func (c *compiler) compileForStmt(stmt *ast.ForStmt) error {
	c.emit(taivm.OpEnterScope)
	c.scopeDepth++
	defer func() {
		c.emit(taivm.OpLeaveScope)
		c.scopeDepth--
	}()

	if stmt.Init != nil {
		if err := c.compileStmt(stmt.Init); err != nil {
			return err
		}
	}

	loop := c.enterLoop()
	loop.startPos = len(c.code)

	if stmt.Cond != nil {
		if err := c.compileExpr(stmt.Cond); err != nil {
			return err
		}
		loop.breakTargets = append(loop.breakTargets, c.emitJump(taivm.OpJumpFalse))
	}

	if err := c.compileBlockStmt(stmt.Body); err != nil {
		return err
	}

	if stmt.Post != nil {
		loop.postPos = len(c.code)
		if err := c.compileStmt(stmt.Post); err != nil {
			return err
		}
	}

	c.emitJump(taivm.OpJump.With(loop.startPos - len(c.code) - 1)) // Jump back (relative)

	c.leaveLoop()
	return nil
}

func (c *compiler) compileRangeStmt(stmt *ast.RangeStmt) error {
	if stmt.Value != nil {
		return fmt.Errorf("range with value is not supported (single variable only)")
	}

	if err := c.compileExpr(stmt.X); err != nil {
		return err
	}

	c.emit(taivm.OpGetIter)

	c.emit(taivm.OpEnterScope)
	c.scopeDepth++
	defer func() {
		c.emit(taivm.OpLeaveScope)
		c.scopeDepth--
	}()

	loop := c.enterLoop()
	loop.isRange = true
	loop.startPos = len(c.code)

	// NextIter takes jump offset to end as argument
	nextIterIdx := c.emitJump(taivm.OpNextIter)

	// Assign value to Key (taivm yields element/key)
	if stmt.Key != nil {
		if err := c.compileAssignLHS(stmt.Key, stmt.Tok); err != nil {
			return err
		}
	} else {
		c.emit(taivm.OpPop)
	}

	if err := c.compileBlockStmt(stmt.Body); err != nil {
		return err
	}

	c.emitJump(taivm.OpJump.With(loop.startPos - len(c.code) - 1))

	// Patch NextIter to jump here
	c.patchJump(nextIterIdx, len(c.code))
	c.leaveLoop()

	return nil
}

func (c *compiler) compileBranchStmt(stmt *ast.BranchStmt) error {
	if len(c.loops) == 0 {
		return fmt.Errorf("branch statement outside loop")
	}
	scope := c.loops[len(c.loops)-1]
	unwind := c.scopeDepth - scope.entryDepth
	for range unwind {
		c.emit(taivm.OpLeaveScope)
	}

	switch stmt.Tok {
	case token.BREAK:
		if scope.isRange {
			c.emit(taivm.OpPop)
		}
		scope.breakTargets = append(scope.breakTargets, c.emitJump(taivm.OpJump))
	case token.CONTINUE:
		scope.continueTargets = append(scope.continueTargets, c.emitJump(taivm.OpJump))
	default:
		return fmt.Errorf("unsupported branch token: %s", stmt.Tok)
	}
	return nil
}

func (c *compiler) compileAssignLHS(expr ast.Expr, tok token.Token) error {
	if ident, ok := expr.(*ast.Ident); ok {
		if ident.Name == "_" {
			c.emit(taivm.OpPop)
		} else {
			idx := c.addConst(ident.Name)
			if tok == token.DEFINE {
				c.emit(taivm.OpDefVar.With(idx))
			} else {
				c.emit(taivm.OpSetVar.With(idx))
			}
		}
		return nil
	}
	return fmt.Errorf("complex assignment in range not supported")
}

func (c *compiler) compileGenDecl(decl *ast.GenDecl) error {
	for _, spec := range decl.Specs {
		switch s := spec.(type) {

		case *ast.ImportSpec:
			path, err := strconv.Unquote(s.Path.Value)
			if err != nil {
				return err
			}
			idx := c.addConst(path)
			c.emit(taivm.OpImport.With(idx))

		case *ast.ValueSpec:
			if len(s.Values) == 1 && len(s.Names) > 1 {
				// var a, b = f()
				if err := c.compileExpr(s.Values[0]); err != nil {
					return err
				}
				c.emit(taivm.OpUnpack.With(len(s.Names)))
				// OpUnpack puts 1st element at top
				for i := 0; i < len(s.Names); i++ {
					name := s.Names[i]
					idx := c.addConst(name.Name)
					c.emit(taivm.OpDefVar.With(idx))
				}

			} else {
				// var a = 1
				// var a, b = 1, 2
				for i, name := range s.Names {
					if i < len(s.Values) {
						if err := c.compileExpr(s.Values[i]); err != nil {
							return err
						}
					} else {
						c.loadConst(nil)
					}
					idx := c.addConst(name.Name)
					c.emit(taivm.OpDefVar.With(idx))
				}
			}

		}
	}
	return nil
}

func (c *compiler) typeToString(expr ast.Expr) (string, error) {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name, nil
	case *ast.SelectorExpr:
		x, err := c.typeToString(e.X)
		if err != nil {
			return "", err
		}
		return x + "." + e.Sel.Name, nil
	case *ast.StarExpr:
		x, err := c.typeToString(e.X)
		if err != nil {
			return "", err
		}
		return "*" + x, nil
	case *ast.ArrayType:
		elt, err := c.typeToString(e.Elt)
		if err != nil {
			return "", err
		}
		return "[]" + elt, nil
	case *ast.MapType:
		key, err := c.typeToString(e.Key)
		if err != nil {
			return "", err
		}
		val, err := c.typeToString(e.Value)
		if err != nil {
			return "", err
		}
		return "map[" + key + "]" + val, nil
	case *ast.InterfaceType:
		return "interface{}", nil
	case *ast.ChanType:
		return "chan", nil
	default:
		return "", fmt.Errorf("unknown type expr: %T", expr)
	}
}
