package taigo

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strconv"

	"github.com/reusee/tai/taivm"
)

type compiler struct {
	name       string
	code       []taivm.OpCode
	constants  []any
	loops      []*loopScope
	scopeDepth int
	tmpCount   int
	params     map[string]int
	iotaVal    int
	labels     map[string]int
	unresolved map[string][]int
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

func (c *compiler) nextTmp() string {
	c.tmpCount++
	return fmt.Sprintf("$tmp%d", c.tmpCount)
}

func (c *compiler) loadConst(val any) {
	idx := c.addConst(val)
	c.emit(taivm.OpLoadConst.With(idx))
}

func (c *compiler) compileFile(file *ast.File) error {
	var hasMain bool

	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Recv == nil && funcDecl.Name.Name == "init" {
				if err := c.compileInitFunc(funcDecl); err != nil {
					return err
				}
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
		if d.Tok == token.VAR || d.Tok == token.CONST || d.Tok == token.IMPORT || d.Tok == token.TYPE {
			return c.compileGenDecl(d)
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

	name := decl.Name.Name
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		var typeName string
		var isPointer bool
		switch t := recv.Type.(type) {
		case *ast.Ident:
			typeName = t.Name
		case *ast.StarExpr:
			if id, ok := t.X.(*ast.Ident); ok {
				typeName = id.Name
				isPointer = true
			}
		}
		if typeName != "" {
			if isPointer {
				name = "*" + typeName + "." + name
			} else {
				name = typeName + "." + name
			}
		}
	}

	nameIdx := c.addConst(name)
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
		name:       decl.Name.Name,
		params:     make(map[string]int),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}
	if err := sub.setupParams(decl); err != nil {
		return nil, err
	}
	for _, stmt := range decl.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return nil, err
		}
	}
	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)
	if err := sub.resolveLabels(); err != nil {
		return nil, err
	}
	fn := sub.getFunction()
	if err := c.setFuncMetadata(fn, decl); err != nil {
		return nil, err
	}
	return fn, nil
}

func (c *compiler) setupParams(decl *ast.FuncDecl) error {
	idx := 0
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		for _, name := range recv.Names {
			c.params[name.Name] = idx
			idx++
		}
		if len(recv.Names) == 0 {
			idx++
		}
	}
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			for _, name := range field.Names {
				c.params[name.Name] = idx
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
	}
	return nil
}

func (c *compiler) resolveLabels() error {
	for name, indices := range c.unresolved {
		target, ok := c.labels[name]
		if !ok {
			return fmt.Errorf("label %s not defined", name)
		}
		for _, idx := range indices {
			c.patchJump(idx, target)
		}
	}
	return nil
}

func (c *compiler) setFuncMetadata(fn *taivm.Function, decl *ast.FuncDecl) error {
	tObj, err := c.resolveType(decl.Type)
	if err != nil {
		return err
	}
	ft := tObj.(reflect.Type)
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		rtObj, err := c.resolveType(recv.Type)
		if err != nil {
			return err
		}
		rt := rtObj.(reflect.Type)
		ins := make([]reflect.Type, 0, ft.NumIn()+1)
		ins = append(ins, rt)
		for i := 0; i < ft.NumIn(); i++ {
			ins = append(ins, ft.In(i))
		}
		outs := make([]reflect.Type, ft.NumOut())
		for i := 0; i < ft.NumOut(); i++ {
			outs[i] = ft.Out(i)
		}
		ft = reflect.FuncOf(ins, outs, ft.IsVariadic())
	}
	fn.Type = ft
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		for _, name := range recv.Names {
			fn.ParamNames = append(fn.ParamNames, name.Name)
		}
		if len(recv.Names) == 0 {
			fn.ParamNames = append(fn.ParamNames, "")
		}
	}
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

	case *ast.StarExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		c.emit(taivm.OpDeref)
		return nil

	case *ast.TypeAssertExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		if e.Type == nil {
			return fmt.Errorf("type switch not supported")
		}
		if err := c.compileExpr(e.Type); err != nil {
			return err
		}
		c.emit(taivm.OpTypeAssert)
		return nil

	case *ast.KeyValueExpr:
		return fmt.Errorf("key:value expression not supported outside composite literal")

	case *ast.Ellipsis:
		return fmt.Errorf("ellipsis expression not supported outside call")

	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		t, err := c.resolveType(e)
		if err != nil {
			return err
		}
		c.loadConst(t)
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
		v, err := strconv.Unquote(expr.Value)
		if err != nil {
			return err
		}
		runes := []rune(v)
		if len(runes) != 1 {
			return fmt.Errorf("invalid char literal: %s", expr.Value)
		}
		c.loadConst(int64(runes[0]))

	default:
		return fmt.Errorf("unknown basic lit kind: %v", expr.Kind)
	}

	return nil
}

func (c *compiler) compileIdentifier(expr *ast.Ident) error {
	switch expr.Name {

	case "iota":
		c.loadConst(int64(c.iotaVal))

	case "true":
		c.loadConst(true)
	case "false":
		c.loadConst(false)

	case "nil":
		c.loadConst(nil)

	case "int",
		"int8",
		"int16",
		"int32", "rune",
		"int64",
		"uint",
		"uint8", "byte",
		"uint16",
		"uint32",
		"uint64",
		"float32",
		"float64",
		"string",
		"any",
		"error",
		"bool",
		"complex64",
		"complex128":
		t, err := c.resolveType(expr)
		if err != nil {
			return err
		}
		c.loadConst(t)

	default:
		// Check if it's a parameter in the current function scope
		if idx, ok := c.params[expr.Name]; ok {
			c.emit(taivm.OpGetLocal.With(idx))
		} else {
			idx := c.addConst(expr.Name)
			c.emit(taivm.OpLoadVar.With(idx))
		}
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
		switch x := expr.X.(type) {
		case *ast.Ident:
			idx := c.addConst(x.Name)
			c.emit(taivm.OpAddrOf.With(idx))
		case *ast.IndexExpr:
			if err := c.compileExpr(x.X); err != nil {
				return err
			}
			if err := c.compileExpr(x.Index); err != nil {
				return err
			}
			c.emit(taivm.OpAddrOfIndex)
		case *ast.SelectorExpr:
			if err := c.compileExpr(x.X); err != nil {
				return err
			}
			c.loadConst(x.Sel.Name)
			c.emit(taivm.OpAddrOfAttr)
		default:
			return fmt.Errorf("cannot take address of %T", expr.X)
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
		name:       "anon",
		params:     make(map[string]int),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}

	if expr.Type.Params != nil {
		idx := 0
		for _, field := range expr.Type.Params.List {
			for _, name := range field.Names {
				sub.params[name.Name] = idx
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
	}

	for _, stmt := range expr.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return err
		}
	}

	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)

	for name, indices := range sub.unresolved {
		target, ok := sub.labels[name]
		if !ok {
			return fmt.Errorf("label %s not defined", name)
		}
		for _, idx := range indices {
			sub.patchJump(idx, target)
		}
	}

	fn := sub.getFunction()
	tObj, err := c.resolveType(expr.Type)
	if err != nil {
		return err
	}
	fn.Type = tObj.(reflect.Type)
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
	switch t := expr.Type.(type) {
	case *ast.ArrayType:
		for _, elt := range expr.Elts {
			if err := c.compileExpr(elt); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(expr.Elts)))

	case *ast.Ident, *ast.SelectorExpr:
		var typeName string
		var rawName string
		if ident, ok := t.(*ast.Ident); ok {
			typeName = ident.Name
			rawName = ident.Name
		} else {
			sel := t.(*ast.SelectorExpr)
			if id, ok := sel.X.(*ast.Ident); ok {
				typeName = id.Name + "." + sel.Sel.Name
			} else {
				typeName = sel.Sel.Name
			}
			rawName = sel.Sel.Name
		}
		for _, elt := range expr.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return fmt.Errorf("struct element must be key:value")
			}
			if ident, ok := kv.Key.(*ast.Ident); ok {
				c.loadConst(ident.Name)
			} else {
				if err := c.compileExpr(kv.Key); err != nil {
					return err
				}
			}
			if err := c.compileExpr(kv.Value); err != nil {
				return err
			}
		}
		c.loadConst(typeName)
		idx := c.addConst("_embedded_info_" + rawName)
		c.emit(taivm.OpLoadVar.With(idx))
		c.emit(taivm.OpMakeStruct.With(len(expr.Elts)))

	default:
		for _, elt := range expr.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return fmt.Errorf("element must be key:value")
			}
			if ident, ok := kv.Key.(*ast.Ident); ok {
				c.loadConst(ident.Name)
			} else {
				if err := c.compileExpr(kv.Key); err != nil {
					return err
				}
			}
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
		c.labels[s.Label.Name] = len(c.code)
		return c.compileStmt(s.Stmt)

	case *ast.DeferStmt:
		return c.compileDeferStmt(s)

	case *ast.GoStmt:
		return fmt.Errorf("go statement not supported")

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
	if len(stmt.Rhs) == 1 {
		rhs := stmt.Rhs[0]
		// Handle v, ok := x.(T)
		if ta, ok := rhs.(*ast.TypeAssertExpr); ok && len(stmt.Lhs) == 2 {
			if err := c.compileExpr(ta.X); err != nil {
				return err
			}
			if err := c.compileExpr(ta.Type); err != nil {
				return err
			}
			c.emit(taivm.OpTypeAssertOk)
			c.emit(taivm.OpSwap) // Fix stack order for TypeAssertOk
		} else {
			if err := c.compileExpr(rhs); err != nil {
				return err
			}
			c.emit(taivm.OpUnpack.With(len(stmt.Lhs)))
		}
		for i := 0; i < len(stmt.Lhs); i++ {
			if err := c.compileAssignFromStack(stmt.Lhs[i], stmt.Tok); err != nil {
				return err
			}
		}
	} else if len(stmt.Rhs) == len(stmt.Lhs) {
		for _, r := range stmt.Rhs {
			if err := c.compileExpr(r); err != nil {
				return err
			}
		}
		for i := len(stmt.Lhs) - 1; i >= 0; i-- {
			if err := c.compileAssignFromStack(stmt.Lhs[i], stmt.Tok); err != nil {
				return err
			}
		}
	} else {
		return fmt.Errorf("assignment count mismatch: %d = %d", len(stmt.Lhs), len(stmt.Rhs))
	}
	return nil
}

func (c *compiler) compileSingleAssign(lhs, rhs ast.Expr, tok token.Token) error {
	if err := c.compileExpr(rhs); err != nil {
		return err
	}
	return c.compileAssignFromStack(lhs, tok)
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
	if !ok || (decl.Tok != token.VAR && decl.Tok != token.CONST && decl.Tok != token.TYPE) {
		return fmt.Errorf("unsupported decl in function body")
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

		hasFallthrough := false
		if len(cc.Body) > 0 {
			if br, ok := cc.Body[len(cc.Body)-1].(*ast.BranchStmt); ok && br.Tok == token.FALLTHROUGH {
				hasFallthrough = true
			}
		}

		if !hasFallthrough {
			endJumps = append(endJumps, c.emitJump(taivm.OpJump))
		}
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
	if err := c.compileExpr(stmt.X); err != nil {
		return err
	}
	containerTmp := c.nextTmp()
	containerIdx := c.addConst(containerTmp)
	c.emit(taivm.OpDefVar.With(containerIdx))

	c.emit(taivm.OpLoadVar.With(containerIdx))
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

	nextIterIdx := c.emitJump(taivm.OpNextIter)

	if stmt.Value != nil {
		c.emit(taivm.OpDup)
		c.emit(taivm.OpLoadVar.With(containerIdx))
		c.emit(taivm.OpSwap)
		c.emit(taivm.OpGetIndex)
		if err := c.compileAssignFromStack(stmt.Value, stmt.Tok); err != nil {
			return err
		}
	}

	if stmt.Key != nil {
		if err := c.compileAssignFromStack(stmt.Key, stmt.Tok); err != nil {
			return err
		}
	} else {
		c.emit(taivm.OpPop)
	}

	if err := c.compileBlockStmt(stmt.Body); err != nil {
		return err
	}

	c.emitJump(taivm.OpJump.With(loop.startPos - len(c.code) - 1))

	c.patchJump(nextIterIdx, len(c.code))
	c.leaveLoop()

	return nil
}

func (c *compiler) compileBranchStmt(stmt *ast.BranchStmt) error {
	if stmt.Tok == token.GOTO {
		if stmt.Label == nil {
			return fmt.Errorf("goto requires label")
		}
		name := stmt.Label.Name
		if target, ok := c.labels[name]; ok {
			c.emitJump(taivm.OpJump.With(target - len(c.code) - 1))
		} else {
			idx := c.emitJump(taivm.OpJump)
			c.unresolved[name] = append(c.unresolved[name], idx)
		}
		return nil
	}

	if stmt.Tok == token.FALLTHROUGH {
		return nil // implementation-specific: SwitchStmt handles it
	}

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

func (c *compiler) compileDeferStmt(stmt *ast.DeferStmt) error {
	sub := &compiler{
		name:       "defer",
		params:     make(map[string]int),
		labels:     make(map[string]int),
		unresolved: make(map[string][]int),
	}

	if err := sub.compileExprStmt(&ast.ExprStmt{X: stmt.Call}); err != nil {
		return err
	}

	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)

	for name, indices := range sub.unresolved {
		target, ok := sub.labels[name]
		if !ok {
			return fmt.Errorf("label %s not defined", name)
		}
		for _, idx := range indices {
			sub.patchJump(idx, target)
		}
	}

	fn := sub.getFunction()
	idx := c.addConst(fn)
	c.emit(taivm.OpMakeClosure.With(idx))
	c.emit(taivm.OpDefer)
	return nil
}

func (c *compiler) compileGenDecl(decl *ast.GenDecl) error {
	isConst := decl.Tok == token.CONST
	var lastExprs []ast.Expr
	for i, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.ImportSpec:
			path, err := strconv.Unquote(s.Path.Value)
			if err != nil {
				return err
			}
			idx := c.addConst(path)
			c.emit(taivm.OpImport.With(idx))
			c.emit(taivm.OpPop)
		case *ast.ValueSpec:
			if isConst {
				c.iotaVal = i
			}
			values := s.Values
			if isConst && len(values) == 0 {
				values = lastExprs
			} else if isConst {
				lastExprs = values
			}
			if len(values) == 1 && len(s.Names) > 1 {
				rhs := values[0]
				// Handle var v, ok = x.(T)
				if ta, ok := rhs.(*ast.TypeAssertExpr); ok && len(s.Names) == 2 {
					if err := c.compileExpr(ta.X); err != nil {
						return err
					}
					if err := c.compileExpr(ta.Type); err != nil {
						return err
					}
					c.emit(taivm.OpTypeAssertOk)
					c.emit(taivm.OpSwap) // Fix stack order for TypeAssertOk
				} else {
					if err := c.compileExpr(rhs); err != nil {
						return err
					}
					c.emit(taivm.OpUnpack.With(len(s.Names)))
				}
				for i := 0; i < len(s.Names); i++ {
					name := s.Names[i]
					if name.Name == "_" {
						c.emit(taivm.OpPop)
						continue
					}
					idx := c.addConst(name.Name)
					c.emit(taivm.OpDefVar.With(idx))
				}
			} else {
				if len(values) > 0 && len(values) != len(s.Names) {
					return fmt.Errorf("assignment count mismatch: %d = %d", len(s.Names), len(values))
				}
				for i, name := range s.Names {
					if name.Name == "_" {
						if i < len(values) {
							if err := c.compileExpr(values[i]); err != nil {
								return err
							}
						} else {
							c.loadConst(nil)
						}
						c.emit(taivm.OpPop)
						continue
					}
					if i < len(values) {
						if err := c.compileExpr(values[i]); err != nil {
							return err
						}
					} else {
						c.loadConst(nil)
					}
					idx := c.addConst(name.Name)
					c.emit(taivm.OpDefVar.With(idx))
				}
			}
		case *ast.TypeSpec:
			if err := c.compileTypeSpec(s); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compiler) compileTypeSpec(spec *ast.TypeSpec) error {
	t, err := c.resolveType(spec.Type)
	if err != nil {
		return err
	}
	c.loadConst(t)
	idx := c.addConst(spec.Name.Name)
	c.emit(taivm.OpDefVar.With(idx))
	// Record embedding information for struct field promotion
	if st, ok := spec.Type.(*ast.StructType); ok {
		var embedded []any
		for _, field := range st.Fields.List {
			if len(field.Names) == 0 {
				switch ft := field.Type.(type) {
				case *ast.Ident:
					embedded = append(embedded, ft.Name)
				case *ast.SelectorExpr:
					embedded = append(embedded, ft.Sel.Name)
				case *ast.StarExpr:
					if id, ok := ft.X.(*ast.Ident); ok {
						embedded = append(embedded, id.Name)
					} else if sel, ok := ft.X.(*ast.SelectorExpr); ok {
						embedded = append(embedded, sel.Sel.Name)
					}
				}
			}
		}
		if len(embedded) > 0 {
			idx := c.addConst("_embedded_info_" + spec.Name.Name)
			for _, e := range embedded {
				c.loadConst(e)
			}
			c.emit(taivm.OpMakeList.With(len(embedded)))
			c.emit(taivm.OpDefVar.With(idx))
		}
	}
	return nil
}

func (c *compiler) resolveType(expr ast.Expr) (any, error) {
	switch e := expr.(type) {

	case *ast.Ident:
		switch e.Name {

		case "int":
			return reflect.TypeFor[int64](), nil
		case "int8":
			return reflect.TypeFor[int8](), nil
		case "int16":
			return reflect.TypeFor[int16](), nil
		case "int32", "rune":
			return reflect.TypeFor[int32](), nil
		case "int64":
			return reflect.TypeFor[int64](), nil

		case "uint":
			return reflect.TypeFor[uint64](), nil
		case "uint8", "byte":
			return reflect.TypeFor[uint8](), nil
		case "uint16":
			return reflect.TypeFor[uint16](), nil
		case "uint32":
			return reflect.TypeFor[uint32](), nil
		case "uint64":
			return reflect.TypeFor[uint64](), nil

		case "float32":
			return reflect.TypeFor[float32](), nil
		case "float64":
			return reflect.TypeFor[float64](), nil

		case "string":
			return reflect.TypeFor[string](), nil

		case "any":
			return reflect.TypeFor[any](), nil
		case "error":
			return reflect.TypeFor[error](), nil

		case "bool":
			return reflect.TypeFor[bool](), nil

		case "complex64":
			return reflect.TypeFor[complex64](), nil
		case "complex128":
			return reflect.TypeFor[complex128](), nil

		default:
			// Treat unknown identifiers as any to allow compilation of user-defined types and embedding.
			// The VM handles actual field/method resolution dynamically by name.
			return reflect.TypeFor[any](), nil
		}

	case *ast.ArrayType:
		eltObj, err := c.resolveType(e.Elt)
		if err != nil {
			return nil, err
		}
		elt := eltObj.(reflect.Type)
		if e.Len != nil {
			var length int
			if lit, ok := e.Len.(*ast.BasicLit); ok && lit.Kind == token.INT {
				i, _ := strconv.Atoi(lit.Value)
				length = i
			}
			return reflect.ArrayOf(length, elt), nil
		} else {
			return reflect.SliceOf(elt), nil
		}

	case *ast.Ellipsis:
		if e.Elt == nil {
			return reflect.TypeFor[[]any](), nil
		}
		eltObj, err := c.resolveType(e.Elt)
		if err != nil {
			return nil, err
		}
		return reflect.SliceOf(eltObj.(reflect.Type)), nil

	case *ast.ChanType:
		tObj, err := c.resolveType(e.Value)
		if err != nil {
			return nil, err
		}
		t := tObj.(reflect.Type)
		switch e.Dir {
		case ast.RECV | ast.SEND:
			return reflect.ChanOf(reflect.BothDir, t), nil
		case ast.RECV:
			return reflect.ChanOf(reflect.RecvDir, t), nil
		case ast.SEND:
			return reflect.ChanOf(reflect.SendDir, t), nil
		default:
			return nil, fmt.Errorf("unknown dir: %v", e.Dir)
		}

	case *ast.FuncType:
		var params []reflect.Type
		if e.Params != nil {
			for _, field := range e.Params.List {
				tObj, err := c.resolveType(field.Type)
				if err != nil {
					return nil, err
				}
				t := tObj.(reflect.Type)
				n := len(field.Names)
				if n == 0 {
					n = 1
				}
				for range n {
					params = append(params, t)
				}
			}
		}
		var results []reflect.Type
		if e.Results != nil {
			for _, field := range e.Results.List {
				tObj, err := c.resolveType(field.Type)
				if err != nil {
					return nil, err
				}
				t := tObj.(reflect.Type)
				n := len(field.Names)
				if n == 0 {
					n = 1
				}
				for range n {
					results = append(results, t)
				}
			}
		}
		variadic := false
		if e.Params != nil && len(e.Params.List) > 0 {
			last := e.Params.List[len(e.Params.List)-1]
			if _, ok := last.Type.(*ast.Ellipsis); ok {
				variadic = true
			}
		}
		return reflect.FuncOf(params, results, variadic), nil

	case *ast.MapType:
		ktObj, err := c.resolveType(e.Key)
		if err != nil {
			return nil, err
		}
		kt := ktObj.(reflect.Type)
		vtObj, err := c.resolveType(e.Value)
		if err != nil {
			return nil, err
		}
		vt := vtObj.(reflect.Type)
		return reflect.MapOf(kt, vt), nil

	case *ast.StarExpr:
		tObj, err := c.resolveType(e.X)
		if err != nil {
			return nil, err
		}
		t := tObj.(reflect.Type)
		return reflect.PointerTo(t), nil

	case *ast.StructType:
		var fields []reflect.StructField
		if e.Fields != nil {
			for _, field := range e.Fields.List {
				ftObj, err := c.resolveType(field.Type)
				if err != nil {
					return nil, err
				}
				ft := ftObj.(reflect.Type)
				if len(field.Names) == 0 {
					// Handle embedded fields
					var name string
					switch t := field.Type.(type) {
					case *ast.Ident:
						name = t.Name
					case *ast.StarExpr:
						if id, ok := t.X.(*ast.Ident); ok {
							name = id.Name
						}
					}
					if name != "" {
						sf := reflect.StructField{
							Name:      name,
							Type:      ft,
							Anonymous: true,
						}
						if name[0] >= 'a' && name[0] <= 'z' {
							sf.PkgPath = "main"
						}
						fields = append(fields, sf)
					}
				}
				for _, name := range field.Names {
					sf := reflect.StructField{
						Name: name.Name,
						Type: ft,
					}
					if name.Name[0] >= 'a' && name.Name[0] <= 'z' {
						sf.PkgPath = "main"
					}
					fields = append(fields, sf)
				}
			}
		}
		return reflect.StructOf(fields), nil

	case *ast.InterfaceType:
		methods := make(map[string]reflect.Type)
		if e.Methods != nil {
			for _, field := range e.Methods.List {
				tObj, err := c.resolveType(field.Type)
				if err != nil {
					return nil, err
				}
				ft := tObj.(reflect.Type)
				for _, name := range field.Names {
					methods[name.Name] = ft
				}
			}
		}
		return &taivm.Interface{Methods: methods}, nil

	case *ast.SelectorExpr:
		// Go reflect doesn't support creating interfaces with methods at runtime.
		// For now, return any to allow compilation.
		// VM level interface check might be limited or require custom logic.
		return reflect.TypeFor[any](), nil

	default:
		return nil, fmt.Errorf("unsupported type expression: %T", expr)
	}
}

func (c *compiler) compileAssignFromStack(lhs ast.Expr, tok token.Token) error {
	switch e := lhs.(type) {
	case *ast.Ident:
		if e.Name == "_" {
			c.emit(taivm.OpPop)
			return nil
		}
		idx := c.addConst(e.Name)
		if tok == token.DEFINE {
			c.emit(taivm.OpDefVar.With(idx))
		} else {
			c.emit(taivm.OpSetVar.With(idx))
		}
		return nil

	case *ast.IndexExpr, *ast.SelectorExpr:
		// Save value to temp to evaluate LHS components
		tmp := c.nextTmp()
		tmpIdx := c.addConst(tmp)
		c.emit(taivm.OpDefVar.With(tmpIdx))
		if idxExpr, ok := e.(*ast.IndexExpr); ok {
			if err := c.compileExpr(idxExpr.X); err != nil {
				return err
			}
			if err := c.compileExpr(idxExpr.Index); err != nil {
				return err
			}
			c.emit(taivm.OpLoadVar.With(tmpIdx))
			c.emit(taivm.OpSetIndex)
		} else if selExpr, ok := e.(*ast.SelectorExpr); ok {
			if err := c.compileExpr(selExpr.X); err != nil {
				return err
			}
			c.loadConst(selExpr.Sel.Name)
			c.emit(taivm.OpLoadVar.With(tmpIdx))
			c.emit(taivm.OpSetAttr)
		}
		return nil

	case *ast.StarExpr:
		if err := c.compileExpr(e.X); err != nil {
			return err
		}
		c.emit(taivm.OpSwap)
		c.emit(taivm.OpSetDeref)
		return nil

	default:
		return fmt.Errorf("assignment to %T not supported", lhs)
	}
}
