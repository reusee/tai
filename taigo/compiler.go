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
	loops     []*loopScope
}

type loopScope struct {
	breakTargets    []int
	continueTargets []int
	startPos        int
	postPos         int // position of post statement if any
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
	offset := target - idx
	inst := c.code[idx]
	// Preserve opcode, set offset in upper 24 bits
	op := inst & 0xff
	c.code[idx] = op | (taivm.OpCode(offset) << 8)
}

func (c *compiler) enterLoop() *loopScope {
	scope := &loopScope{
		startPos: len(c.code),
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
		return c.compileFuncDecl(d)

	case *ast.GenDecl:
		if d.Tok == token.VAR {
			return c.compileGenDecl(d)
		}
		// TODO const, type

	default:
		return fmt.Errorf("unknown declaration type: %T", decl)

	}

	return nil
}

func (c *compiler) compileFuncDecl(decl *ast.FuncDecl) error {
	sub := &compiler{
		name: decl.Name.Name,
	}

	for _, stmt := range decl.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return err
		}
	}

	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)

	fn := sub.getFunction()
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
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

	nameIdx := c.addConst(decl.Name.Name)
	c.emit(taivm.OpDefVar.With(nameIdx))

	return nil
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

	default:
		return fmt.Errorf("unknown expr type: %T", expr)
	}
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
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	if err := c.compileExpr(expr.Y); err != nil {
		return err
	}

	switch expr.Op {
	case token.ADD:
		c.emit(taivm.OpAdd)
	case token.SUB:
		c.emit(taivm.OpSub)
	case token.MUL:
		c.emit(taivm.OpMul)
	case token.QUO:
		c.emit(taivm.OpDiv)
	case token.REM:
		c.emit(taivm.OpMod)
	case token.EQL:
		c.emit(taivm.OpEq)
	case token.NEQ:
		c.emit(taivm.OpNe)
	case token.LSS:
		c.emit(taivm.OpLt)
	case token.LEQ:
		c.emit(taivm.OpLe)
	case token.GTR:
		c.emit(taivm.OpGt)
	case token.GEQ:
		c.emit(taivm.OpGe)
	case token.AND:
		c.emit(taivm.OpBitAnd)
	case token.OR:
		c.emit(taivm.OpBitOr)
	case token.XOR:
		c.emit(taivm.OpBitXor)
	case token.SHL:
		c.emit(taivm.OpBitLsh)
	case token.SHR:
		c.emit(taivm.OpBitRsh)
	default:
		return fmt.Errorf("unknown binary operator: %s", expr.Op)
	}

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
	switch t := expr.Type.(type) {
	case *ast.ArrayType:
		for _, elt := range expr.Elts {
			if err := c.compileExpr(elt); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(expr.Elts)))

	case *ast.MapType:
		for _, elt := range expr.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return fmt.Errorf("map element must be key:value")
			}
			if err := c.compileExpr(kv.Key); err != nil {
				return err
			}
			if err := c.compileExpr(kv.Value); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeMap.With(len(expr.Elts)))

	default:
		return fmt.Errorf("composite lit type %T not supported", t)
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

	case *ast.ForStmt:
		return c.compileForStmt(s)

	case *ast.RangeStmt:
		return c.compileRangeStmt(s)

	case *ast.BranchStmt:
		return c.compileBranchStmt(s)

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
	for _, stmt := range stmt.List {
		if err := c.compileStmt(stmt); err != nil {
			return err
		}
	}
	c.emit(taivm.OpLeaveScope)
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
	if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
		return fmt.Errorf("only single assignment supported for now")
	}

	lhs := stmt.Lhs[0]
	rhs := stmt.Rhs[0]

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

	if err := c.compileExpr(rhs); err != nil {
		return err
	}

	if ident, ok := lhs.(*ast.Ident); ok {
		idx := c.addConst(ident.Name)
		if stmt.Tok == token.DEFINE {
			c.emit(taivm.OpDefVar.With(idx))
		} else {
			c.emit(taivm.OpSetVar.With(idx))
		}
	} else {
		return fmt.Errorf("assignment to %T not supported", lhs)
	}

	return nil
}

func (c *compiler) compileIncDecStmt(stmt *ast.IncDecStmt) error {
	ident, ok := stmt.X.(*ast.Ident)
	if !ok {
		return fmt.Errorf("inc/dec only supported on identifiers")
	}
	idx := c.addConst(ident.Name)
	c.emit(taivm.OpLoadVar.With(idx))
	c.loadConst(1)
	if stmt.Tok == token.INC {
		c.emit(taivm.OpAdd)
	} else {
		c.emit(taivm.OpSub)
	}
	c.emit(taivm.OpSetVar.With(idx))
	return nil
}

func (c *compiler) compileDeclStmt(stmt *ast.DeclStmt) error {
	decl, ok := stmt.Decl.(*ast.GenDecl)
	if !ok || decl.Tok != token.VAR {
		return fmt.Errorf("only var decls supported in function body")
	}
	return c.compileGenDecl(decl)
}

func (c *compiler) compileIfStmt(stmt *ast.IfStmt) error {
	if stmt.Init != nil {
		c.emit(taivm.OpEnterScope)
		if err := c.compileStmt(stmt.Init); err != nil {
			return err
		}
		defer c.emit(taivm.OpLeaveScope)
	}

	if err := c.compileExpr(stmt.Cond); err != nil {
		return err
	}

	jumpElse := c.emitJump(taivm.OpJumpFalse)

	if err := c.compileBlockStmt(stmt.Body); err != nil {
		return err
	}

	jumpEnd := c.emitJump(taivm.OpJump)

	c.patchJump(jumpElse, len(c.code))

	if stmt.Else != nil {
		if err := c.compileStmt(stmt.Else); err != nil {
			return err
		}
	}

	c.patchJump(jumpEnd, len(c.code))
	return nil
}

func (c *compiler) compileForStmt(stmt *ast.ForStmt) error {
	c.emit(taivm.OpEnterScope)
	defer c.emit(taivm.OpLeaveScope)

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
	if err := c.compileExpr(stmt.X); err != nil {
		return err
	}

	c.emit(taivm.OpGetIter)

	loop := c.enterLoop()
	loop.startPos = len(c.code)

	// NextIter takes jump offset to end as argument
	// We'll treat it as a jump-false-like instruction here but it's embedded
	nextIterIdx := c.emitJump(taivm.OpNextIter)

	// Assign value
	if stmt.Value != nil {
		// If 2 vars (Key, Value), taivm iter usually yields one.
		// For map it yields key. logic below assumes simple iteration
		if err := c.compileAssignLHS(stmt.Value); err != nil {
			return err
		}
	} else if stmt.Key != nil {
		if err := c.compileAssignLHS(stmt.Key); err != nil {
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

	// Pop iterator
	// Note: OpNextIter pops iterator itself when finished
	return nil
}

func (c *compiler) compileBranchStmt(stmt *ast.BranchStmt) error {
	if len(c.loops) == 0 {
		return fmt.Errorf("branch statement outside loop")
	}
	scope := c.loops[len(c.loops)-1]

	switch stmt.Tok {
	case token.BREAK:
		scope.breakTargets = append(scope.breakTargets, c.emitJump(taivm.OpJump))
	case token.CONTINUE:
		scope.continueTargets = append(scope.continueTargets, c.emitJump(taivm.OpJump))
	default:
		return fmt.Errorf("unsupported branch token: %s", stmt.Tok)
	}
	return nil
}

func (c *compiler) compileAssignLHS(expr ast.Expr) error {
	if ident, ok := expr.(*ast.Ident); ok {
		idx := c.addConst(ident.Name)
		if ident.Name == "_" {
			c.emit(taivm.OpPop)
		} else {
			c.emit(taivm.OpSetVar.With(idx))
		}
		return nil
	}
	// Simplified assignment for range
	return fmt.Errorf("complex assignment in range not fully supported yet")
}

func (c *compiler) compileGenDecl(decl *ast.GenDecl) error {
	for _, spec := range decl.Specs {
		vSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		// Handle values
		for i, name := range vSpec.Names {
			if i < len(vSpec.Values) {
				if err := c.compileExpr(vSpec.Values[i]); err != nil {
					return err
				}
			} else {
				c.loadConst(nil)
			}
			idx := c.addConst(name.Name)
			c.emit(taivm.OpDefVar.With(idx))
		}
	}
	return nil
}
