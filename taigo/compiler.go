package taigo

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"maps"
	"math"
	"math/big"
	"reflect"
	"strconv"

	"github.com/reusee/tai/taivm"
)

type compiler struct {
	name           string
	isFunc         bool
	code           []taivm.OpCode
	constants      []any
	consts         map[any]int // For fast constant lookup
	loops          []*loopScope
	scopeDepth     int
	tmpCount       int
	locals         map[string]int
	iotaVal        int
	labels         map[string]int
	unresolved     map[string][]int
	lastConstExprs []ast.Expr
	resultNames    []string
	generics       map[string]bool // Top-level generic names
	typeParams     map[string]bool // Current function/type parameters
	structFields   map[string][]string
	types          map[string]*taivm.Type
}

type topDecl struct {
	names   []string
	deps    []string
	compile func() error
}

type loopScope struct {
	label           string // label of the statement
	breakTargets    []int
	continueTargets []int
	startPos        int
	postPos         int // position of post statement if any
	entryDepth      int
	isRange         bool
	isLoop          bool // true for for/range, false for switch
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

func (c *compiler) enterLoop(label string) *loopScope {
	scope := &loopScope{
		label:      label,
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
	if c.consts == nil {
		c.consts = make(map[any]int)
	}
	switch v := val.(type) {
	case string:
		if idx, ok := c.consts[v]; ok {
			return idx
		}
	case int:
		if idx, ok := c.consts[v]; ok {
			return idx
		}
	case int64:
		if idx, ok := c.consts[v]; ok {
			return idx
		}
	case bool:
		if idx, ok := c.consts[v]; ok {
			return idx
		}
	case nil:
		if idx, ok := c.consts[nil]; ok {
			return idx
		}
	default:
		rv := reflect.ValueOf(val)
		if rv.IsValid() && rv.Type().Comparable() {
			if idx, ok := c.consts[val]; ok {
				return idx
			}
		} else {
			for i, cv := range c.constants {
				if reflect.DeepEqual(cv, val) {
					return i
				}
			}
		}
	}
	idx := len(c.constants)
	c.constants = append(c.constants, val)
	rv := reflect.ValueOf(val)
	if !rv.IsValid() {
		c.consts[nil] = idx
	} else if rv.Type().Comparable() {
		c.consts[val] = idx
	}
	return idx
}

func (c *compiler) nextTmp() string {
	c.tmpCount++
	return fmt.Sprintf("$tmp%d", c.tmpCount)
}

func (c *compiler) evalInt(expr ast.Expr) (int64, bool) {
	val, ok := c.evalConst(expr)
	if !ok {
		return 0, false
	}
	return taivm.ToInt64(val)
}

func (c *compiler) evalConst(expr ast.Expr) (any, bool) {
	val := c.evalConstValue(expr)
	if val == nil || val.Kind() == constant.Unknown {
		return nil, false
	}
	return c.toVMValue(val), true
}

func (c *compiler) evalConstValue(expr ast.Expr) constant.Value {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return constant.MakeFromLiteral(e.Value, e.Kind, 0)
	case *ast.Ident:
		switch e.Name {
		case "true":
			return constant.MakeBool(true)
		case "false":
			return constant.MakeBool(false)
		case "nil":
			return nil
		case "iota":
			return constant.MakeInt64(int64(c.iotaVal))
		}
	case *ast.ParenExpr:
		return c.evalConstValue(e.X)
	case *ast.UnaryExpr:
		x := c.evalConstValue(e.X)
		if x == nil || x.Kind() == constant.Unknown {
			return constant.MakeUnknown()
		}
		return constant.UnaryOp(e.Op, x, 0)
	case *ast.BinaryExpr:
		if e.Op == token.LAND || e.Op == token.LOR {
			return c.evalLogicConst(e)
		}
		x := c.evalConstValue(e.X)
		y := c.evalConstValue(e.Y)
		if x == nil || y == nil || x.Kind() == constant.Unknown || y.Kind() == constant.Unknown {
			return constant.MakeUnknown()
		}
		switch e.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return constant.MakeBool(constant.Compare(x, e.Op, y))
		case token.SHL, token.SHR:
			s, ok := constant.Uint64Val(constant.ToInt(y))
			if !ok {
				return constant.MakeUnknown()
			}
			return constant.Shift(x, e.Op, uint(s))
		}
		return constant.BinaryOp(x, e.Op, y)
	case *ast.CallExpr:
		return c.evalCallConstValue(e)
	}
	return constant.MakeUnknown()
}

func (c *compiler) evalLogicConst(expr *ast.BinaryExpr) constant.Value {
	x := c.evalConstValue(expr.X)
	if x == nil || x.Kind() != constant.Bool {
		return constant.MakeUnknown()
	}
	xv := constant.BoolVal(x)
	if expr.Op == token.LAND {
		if !xv {
			return x
		}
	} else if xv {
		return x
	}
	y := c.evalConstValue(expr.Y)
	if y == nil || y.Kind() != constant.Bool {
		return constant.MakeUnknown()
	}
	return y
}

func (c *compiler) evalCallConstValue(expr *ast.CallExpr) constant.Value {
	id, ok := expr.Fun.(*ast.Ident)
	if !ok || len(expr.Args) == 0 {
		return nil
	}
	switch id.Name {
	case "len":
		if len(expr.Args) == 1 {
			v := c.evalConstValue(expr.Args[0])
			if v != nil && v.Kind() == constant.String {
				return constant.MakeInt64(int64(len(constant.StringVal(v))))
			}
		}
	case "real":
		if len(expr.Args) == 1 {
			v := c.evalConstValue(expr.Args[0])
			if v != nil && v.Kind() != constant.Unknown {
				k := v.Kind()
				if k == constant.Int || k == constant.Float || k == constant.Complex {
					return constant.Real(v)
				}
			}
		}
	case "imag":
		if len(expr.Args) == 1 {
			v := c.evalConstValue(expr.Args[0])
			if v != nil && v.Kind() != constant.Unknown {
				k := v.Kind()
				if k == constant.Int || k == constant.Float || k == constant.Complex {
					return constant.Imag(v)
				}
			}
		}
	case "complex":
		if len(expr.Args) == 2 {
			r := c.evalConstValue(expr.Args[0])
			i := c.evalConstValue(expr.Args[1])
			if r != nil && i != nil && r.Kind() != constant.Unknown && i.Kind() != constant.Unknown {
				rk, ik := r.Kind(), i.Kind()
				if (rk == constant.Int || rk == constant.Float) &&
					(ik == constant.Int || ik == constant.Float) {
					return constant.BinaryOp(r, token.ADD, constant.MakeImag(i))
				}
			}
		}
	}
	return nil
}

func (c *compiler) toVMValue(v constant.Value) any {
	if vi := constant.ToInt(v); vi.Kind() == constant.Int {
		v = vi
	}
	switch v.Kind() {
	case constant.Bool:
		return constant.BoolVal(v)
	case constant.String:
		return constant.StringVal(v)
	case constant.Int:
		return c.toVMIntValue(v)
	case constant.Float:
		return c.toVMFloatValue(v)
	case constant.Complex:
		re, _ := constant.Float64Val(constant.Real(v))
		im, _ := constant.Float64Val(constant.Imag(v))
		return complex(re, im)
	}
	return nil
}

func (c *compiler) toVMIntValue(v constant.Value) any {
	if i, ok := constant.Int64Val(v); ok {
		if int64(int(i)) == i {
			return int(i)
		}
		return i
	}
	return constant.Val(v).(*big.Int)
}

func (c *compiler) toVMFloatValue(v constant.Value) any {
	f, _ := constant.Float64Val(v)
	if !math.IsInf(f, 0) {
		return f
	}
	switch val := constant.Val(v).(type) {
	case *big.Float:
		return val
	case *big.Rat:
		return new(big.Float).SetRat(val)
	}
	return f
}

func (c *compiler) loadConst(val any) {
	idx := c.addConst(val)
	c.emit(taivm.OpLoadConst.With(idx))
}

func (c *compiler) compileFile(file *ast.File) error {
	if c.structFields == nil {
		c.structFields = make(map[string][]string)
	}
	if c.types == nil {
		c.types = make(map[string]*taivm.Type)
	}

	allTopSymbols := make(map[string]bool)
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				continue
			}
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
			} else {
				allTopSymbols[name] = true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					for _, name := range s.Names {
						allTopSymbols[name.Name] = true
					}
				case *ast.TypeSpec:
					allTopSymbols[s.Name.Name] = true
				}
			}
		}
	}

	getDeps := func(node ast.Node) []string {
		var deps []string
		ast.Inspect(node, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if allTopSymbols[id.Name] {
					deps = append(deps, id.Name)
				}
			}
			return true
		})
		return deps
	}

	var funcDecls []topDecl
	var initDecls []func() error
	var varDecls []topDecl
	var hasMain bool

	for _, decl := range file.Decls {
		d := decl
		switch g := d.(type) {
		case *ast.FuncDecl:
			if g.Recv == nil && g.Name.Name == "init" {
				initDecls = append(initDecls, func() error {
					return c.compileInitFunc(g)
				})
			} else {
				if g.Name.Name == "main" {
					hasMain = true
				}
				funcDecls = append(funcDecls, topDecl{
					names: []string{g.Name.Name},
					compile: func() error {
						return c.compileFuncDecl(g)
					},
				})
			}

		case *ast.GenDecl:
			if g.Tok == token.IMPORT {
				if err := c.compileGenDecl(g); err != nil {
					return err
				}
				continue
			}
			var lastExprs []ast.Expr
			for i, spec := range g.Specs {
				s := spec
				var names []string
				var deps []string
				switch v := s.(type) {
				case *ast.ValueSpec:
					for _, n := range v.Names {
						names = append(names, n.Name)
					}
					vals := v.Values
					if g.Tok == token.CONST {
						if len(vals) > 0 {
							lastExprs = vals
						} else {
							vals = lastExprs
						}
					}
					for _, val := range vals {
						deps = append(deps, getDeps(val)...)
					}
				case *ast.TypeSpec:
					names = append(names, v.Name.Name)
					deps = append(deps, getDeps(v.Type)...)
				}
				capturedLastExprs := lastExprs
				capturedIota := i
				varDecls = append(varDecls, topDecl{
					names: names,
					deps:  deps,
					compile: func() error {
						c.iotaVal = capturedIota
						c.lastConstExprs = capturedLastExprs
						switch g.Tok {
						case token.VAR, token.CONST:
							return c.compileValueSpec(s.(*ast.ValueSpec), g.Tok == token.CONST)
						case token.TYPE:
							return c.compileTypeSpec(s.(*ast.TypeSpec))
						}
						return nil
					},
				})
			}
		}
	}

	for _, d := range funcDecls {
		if err := d.compile(); err != nil {
			return err
		}
	}
	sorted, err := sortTopDecls(varDecls)
	if err != nil {
		return err
	}
	for _, d := range sorted {
		if err := d.compile(); err != nil {
			return err
		}
	}
	for _, f := range initDecls {
		if err := f(); err != nil {
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

func sortTopDecls(decls []topDecl) ([]topDecl, error) {
	type node struct {
		decl     topDecl
		deps     []*node
		onStack  bool
		finished bool
	}
	symbolToNode := make(map[string]*node)
	var nodes []*node
	for _, d := range decls {
		n := &node{decl: d}
		nodes = append(nodes, n)
		for _, name := range d.names {
			symbolToNode[name] = n
		}
	}
	for _, n := range nodes {
		for _, depName := range n.decl.deps {
			if depNode, ok := symbolToNode[depName]; ok && depNode != n {
				n.deps = append(n.deps, depNode)
			}
		}
	}
	var result []topDecl
	var visit func(*node) error
	visit = func(n *node) error {
		if n.onStack {
			return nil
		}
		if n.finished {
			return nil
		}
		n.onStack = true
		for _, dep := range n.deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		n.onStack = false
		n.finished = true
		result = append(result, n.decl)
		return nil
	}
	for _, n := range nodes {
		if !n.finished {
			if err := visit(n); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (c *compiler) compileFuncDecl(decl *ast.FuncDecl) error {
	if decl.Type.TypeParams != nil && len(decl.Type.TypeParams.List) > 0 {
		if c.generics == nil {
			c.generics = make(map[string]bool)
		}
		c.generics[decl.Name.Name] = true
	}

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
			} else if idx, ok := t.X.(*ast.IndexExpr); ok {
				if id, ok := idx.X.(*ast.Ident); ok {
					typeName = id.Name
					isPointer = true
				}
			} else if idx, ok := t.X.(*ast.IndexListExpr); ok {
				if id, ok := idx.X.(*ast.Ident); ok {
					typeName = id.Name
					isPointer = true
				}
			}
		case *ast.IndexExpr:
			if id, ok := t.X.(*ast.Ident); ok {
				typeName = id.Name
			}
		case *ast.IndexListExpr:
			if id, ok := t.X.(*ast.Ident); ok {
				typeName = id.Name
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
		name:         decl.Name.Name,
		isFunc:       true,
		consts:       make(map[any]int),
		locals:       make(map[string]int),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		generics:     c.generics,
		typeParams:   make(map[string]bool),
		structFields: c.structFields,
		types:        c.types,
	}
	if decl.Type.TypeParams != nil {
		for _, field := range decl.Type.TypeParams.List {
			for _, name := range field.Names {
				sub.typeParams[name.Name] = true
			}
		}
	}
	if err := sub.setupParams(decl); err != nil {
		return nil, err
	}
	sub.setupResults(decl.Type)
	if err := sub.defineNamedResults(decl.Type); err != nil {
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
	fn.NumLocals = len(sub.locals)
	return fn, nil
}

func (c *compiler) setupParams(decl *ast.FuncDecl) error {
	idx := 0
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		for _, name := range recv.Names {
			c.locals[name.Name] = idx
			idx++
		}
		if len(recv.Names) == 0 {
			idx++
		}
	}
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			for _, name := range field.Names {
				c.locals[name.Name] = idx
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
	}
	return nil
}

func (c *compiler) setupResults(fType *ast.FuncType) {
	if fType.Results != nil {
		for _, field := range fType.Results.List {
			for _, name := range field.Names {
				c.resultNames = append(c.resultNames, name.Name)
			}
		}
	}
}

func (c *compiler) defineNamedResults(fType *ast.FuncType) error {
	if fType.Results != nil {
		for _, field := range fType.Results.List {
			if len(field.Names) == 0 {
				continue
			}
			t, err := c.resolveType(field.Type)
			if err != nil {
				return err
			}
			zero := t.Zero()
			for _, name := range field.Names {
				c.loadConst(zero)
				c.emit(taivm.OpDefVar.With(c.addConst(name.Name)))
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
	ft, err := c.resolveType(decl.Type)
	if err != nil {
		return err
	}
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		rt, err := c.resolveType(recv.Type)
		if err != nil {
			return err
		}
		ft.In = append([]*taivm.Type{rt}, ft.In...)
	}
	fn.Type = ft
	fn.ParamNames = c.getFuncParamNames(decl)
	fn.NumParams = len(fn.ParamNames)
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				fn.Variadic = true
			}
		}
	}
	return nil
}

func (c *compiler) getFuncParamNames(decl *ast.FuncDecl) []string {
	var names []string
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		recv := decl.Recv.List[0]
		for _, name := range recv.Names {
			names = append(names, name.Name)
		}
		if len(recv.Names) == 0 {
			names = append(names, "")
		}
	}
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			if len(field.Names) == 0 {
				names = append(names, "")
			} else {
				for _, name := range field.Names {
					names = append(names, name.Name)
				}
			}
		}
	}
	return names
}

func (c *compiler) compileExpr(expr ast.Expr) error {
	if val, ok := c.evalConst(expr); ok {
		c.loadConst(val)
		return nil
	}
	switch e := expr.(type) {
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
	case *ast.IndexListExpr:
		return c.compileIndexListExpr(e)
	case *ast.SelectorExpr:
		return c.compileSelectorExpr(e)
	case *ast.SliceExpr:
		return c.compileSliceExpr(e)
	case *ast.FuncLit:
		return c.compileFuncLit(e)
	case *ast.CompositeLit:
		return c.compileCompositeLit(e)
	case *ast.StarExpr:
		return c.compileStarExpr(e)
	case *ast.TypeAssertExpr:
		return c.compileTypeAssertExpr(e)
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		return c.compileTypeExpr(e)
	case *ast.KeyValueExpr, *ast.Ellipsis:
		return fmt.Errorf("%T expression not supported here", expr)
	case *ast.BadExpr:
		return fmt.Errorf("bad expression")
	default:
		return fmt.Errorf("unknown expr type: %T", expr)
	}
}

func (c *compiler) compileStarExpr(expr *ast.StarExpr) error {
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	c.emit(taivm.OpDeref)
	return nil
}

func (c *compiler) compileTypeAssertExpr(expr *ast.TypeAssertExpr) error {
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	if expr.Type == nil {
		return fmt.Errorf("type switch not supported")
	}
	if err := c.compileExpr(expr.Type); err != nil {
		return err
	}
	c.emit(taivm.OpTypeAssert)
	return nil
}

func (c *compiler) compileTypeExpr(expr ast.Expr) error {
	t, err := c.resolveType(expr)
	if err != nil {
		return err
	}
	c.loadConst(t)
	return nil
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

func (c *compiler) compileIdentifier(expr *ast.Ident) error {
	name := expr.Name
	switch name {
	case "iota":
		c.loadConst(int64(c.iotaVal))
		return nil
	case "true":
		c.loadConst(true)
		return nil
	case "false":
		c.loadConst(false)
		return nil
	case "nil":
		c.loadConst(nil)
		return nil
	}
	if idx, ok := c.locals[name]; ok {
		c.emit(taivm.OpGetLocal.With(idx))
		return nil
	}
	switch name {
	case "int", "int8", "int16", "int32", "rune", "int64",
		"uint", "uint8", "byte", "uint16", "uint32", "uint64",
		"float32", "float64", "string", "any", "error", "bool",
		"complex64", "complex128":
		t, err := c.resolveType(expr)
		if err != nil {
			return err
		}
		c.loadConst(t)
		return nil
	}
	idx := c.addConst(name)
	c.emit(taivm.OpLoadVar.With(idx))
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

		case *ast.StarExpr:
			if err := c.compileExpr(x.X); err != nil {
				return err
			}
			c.emit(taivm.OpDeref)
			tmpIdx := c.addConst(c.nextTmp())
			c.emit(taivm.OpDefVar.With(tmpIdx))
			c.emit(taivm.OpAddrOf.With(tmpIdx))

		case *ast.CompositeLit:
			if err := c.compileCompositeLit(x); err != nil {
				return err
			}
			tmpIdx := c.addConst(c.nextTmp())
			c.emit(taivm.OpDefVar.With(tmpIdx))
			c.emit(taivm.OpAddrOf.With(tmpIdx))

		case *ast.ParenExpr:
			return c.compileUnaryExpr(&ast.UnaryExpr{Op: token.AND, X: x.X})

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
		return c.compileVariadicCall(expr)
	}
	for _, arg := range expr.Args {
		if err := c.compileExpr(arg); err != nil {
			return err
		}
	}
	c.emit(taivm.OpCall.With(len(expr.Args)))
	return nil
}

func (c *compiler) compileVariadicCall(expr *ast.CallExpr) error {
	numExplicit := len(expr.Args) - 1
	for i := range numExplicit {
		if err := c.compileExpr(expr.Args[i]); err != nil {
			return err
		}
	}
	c.emit(taivm.OpMakeList.With(numExplicit))
	if err := c.compileExpr(expr.Args[numExplicit]); err != nil {
		return err
	}
	c.emit(taivm.OpAdd)
	c.emit(taivm.OpMakeMap.With(0))
	c.emit(taivm.OpCallKw)
	return nil
}

func (c *compiler) compileIndexExpr(expr *ast.IndexExpr) error {
	if id, ok := expr.X.(*ast.Ident); ok && c.generics != nil && c.generics[id.Name] {
		return c.compileExpr(expr.X)
	}
	if err := c.compileExpr(expr.X); err != nil {
		return err
	}
	if err := c.compileExpr(expr.Index); err != nil {
		return err
	}
	c.emit(taivm.OpGetIndex)
	return nil
}

func (c *compiler) compileIndexListExpr(expr *ast.IndexListExpr) error {
	if id, ok := expr.X.(*ast.Ident); ok && c.generics != nil && c.generics[id.Name] {
		return c.compileExpr(expr.X)
	}
	return c.compileExpr(expr.X)
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
	if expr.Low != nil {
		if err := c.compileExpr(expr.Low); err != nil {
			return err
		}
	} else {
		c.loadConst(nil)
	}
	if expr.High != nil {
		if err := c.compileExpr(expr.High); err != nil {
			return err
		}
	} else {
		c.loadConst(nil)
	}
	if expr.Slice3 {
		if expr.Max != nil {
			if err := c.compileExpr(expr.Max); err != nil {
				return err
			}
		} else {
			c.loadConst(nil)
		}
	} else {
		c.loadConst(nil)
	}
	c.emit(taivm.OpGetSlice)
	return nil
}

func (c *compiler) compileFuncLit(expr *ast.FuncLit) error {
	sub := &compiler{
		name:         "anon",
		isFunc:       true,
		consts:       make(map[any]int),
		locals:       make(map[string]int),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		generics:     c.generics,
		typeParams:   c.typeParams,
		structFields: c.structFields,
		types:        c.types,
	}
	if expr.Type.TypeParams != nil && len(expr.Type.TypeParams.List) > 0 {
		newTypeParams := make(map[string]bool)
		maps.Copy(newTypeParams, c.typeParams)
		for _, field := range expr.Type.TypeParams.List {
			for _, name := range field.Names {
				newTypeParams[name.Name] = true
			}
		}
		sub.typeParams = newTypeParams
	}
	if expr.Type.Params != nil {
		idx := 0
		for _, field := range expr.Type.Params.List {
			for _, name := range field.Names {
				sub.locals[name.Name] = idx
				idx++
			}
			if len(field.Names) == 0 {
				idx++
			}
		}
	}
	sub.setupResults(expr.Type)
	if err := sub.defineNamedResults(expr.Type); err != nil {
		return err
	}
	for _, stmt := range expr.Body.List {
		if err := sub.compileStmt(stmt); err != nil {
			return err
		}
	}
	sub.loadConst(nil)
	sub.emit(taivm.OpReturn)
	if err := sub.resolveLabels(); err != nil {
		return err
	}
	fn := sub.getFunction()
	fn.NumLocals = len(sub.locals)
	tObj, err := c.resolveType(expr.Type)
	if err != nil {
		return err
	}
	fn.Type = tObj
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
	if expr.Type == nil {
		return c.compileMapLit(expr)
	}

	if t, err := c.resolveType(expr.Type); err == nil {
		switch t.Kind {
		case taivm.KindSlice, taivm.KindArray:
			return c.compileArrayLit(expr)
		case taivm.KindMap:
			return c.compileMapLit(expr)
		}
	}

	switch t := expr.Type.(type) {
	case *ast.ArrayType:
		return c.compileArrayLit(expr)
	case *ast.Ident, *ast.SelectorExpr:
		return c.compileStructLit(expr, t)
	case *ast.IndexExpr:
		return c.compileStructLit(expr, t.X)
	case *ast.IndexListExpr:
		return c.compileStructLit(expr, t.X)
	default:
		return c.compileMapLit(expr)
	}
}

func (c *compiler) compileArrayLit(expr *ast.CompositeLit) error {
	hasKey := false
	for _, elt := range expr.Elts {
		if _, ok := elt.(*ast.KeyValueExpr); ok {
			hasKey = true
			break
		}
	}

	if !hasKey {
		for _, elt := range expr.Elts {
			if err := c.compileExpr(elt); err != nil {
				return err
			}
		}
		c.emit(taivm.OpMakeList.With(len(expr.Elts)))
		return nil
	}

	c.emit(taivm.OpMakeList.With(0))
	var nextIndex int64 = 0
	var currentLen int64 = 0
	for _, elt := range expr.Elts {
		var idx int64
		var valExpr ast.Expr
		var hasEvalIdx bool

		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			valExpr = kv.Value
			if i, ok := c.evalInt(kv.Key); ok {
				idx = i
				hasEvalIdx = true
			} else {
				// Fallback for dynamic/complex keys
				c.emit(taivm.OpDup)
				if err := c.compileExpr(kv.Key); err != nil {
					return err
				}
				if err := c.compileExpr(kv.Value); err != nil {
					return err
				}
				c.emit(taivm.OpSetIndex)
				continue
			}
		} else {
			idx = nextIndex
			valExpr = elt
			hasEvalIdx = true
		}

		if hasEvalIdx {
			// Pad with nils to reach index
			for currentLen < idx {
				c.loadConst(nil)
				c.emit(taivm.OpListAppend)
				currentLen++
			}
			if idx < currentLen {
				// Overwrite existing index
				c.emit(taivm.OpDup)
				c.loadConst(idx)
				if err := c.compileExpr(valExpr); err != nil {
					return err
				}
				c.emit(taivm.OpSetIndex)
			} else {
				// Append at current end
				if err := c.compileExpr(valExpr); err != nil {
					return err
				}
				c.emit(taivm.OpListAppend)
				currentLen++
			}
			nextIndex = idx + 1
		}
	}
	return nil
}

func (c *compiler) compileStructLit(expr *ast.CompositeLit, t ast.Expr) error {
	var typeName, rawName string
	if ident, ok := t.(*ast.Ident); ok {
		typeName, rawName = ident.Name, ident.Name
	} else if sel, ok := t.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			typeName = id.Name + "." + sel.Sel.Name
		} else {
			typeName = sel.Sel.Name
		}
		rawName = sel.Sel.Name
	}
	numElts := len(expr.Elts)
	if numElts > 0 {
		if _, ok := expr.Elts[0].(*ast.KeyValueExpr); ok {
			for _, elt := range expr.Elts {
				if err := c.compileStructElement(elt); err != nil {
					return err
				}
			}
		} else {
			fields, ok := c.structFields[rawName]
			if !ok {
				return fmt.Errorf("struct fields unknown for unkeyed literal of %s", rawName)
			}
			if len(fields) < numElts {
				return fmt.Errorf("too many values in unkeyed struct literal")
			}
			for i, elt := range expr.Elts {
				c.loadConst(fields[i])
				if err := c.compileExpr(elt); err != nil {
					return err
				}
			}
		}
	}
	c.loadConst(typeName)
	c.emit(taivm.OpLoadVar.With(c.addConst("_embedded_info_" + rawName)))
	c.emit(taivm.OpMakeStruct.With(numElts))
	return nil
}

func (c *compiler) compileStructElement(elt ast.Expr) error {
	kv, ok := elt.(*ast.KeyValueExpr)
	if !ok {
		return fmt.Errorf("struct element must be key:value")
	}
	if ident, ok := kv.Key.(*ast.Ident); ok {
		c.loadConst(ident.Name)
	} else if err := c.compileExpr(kv.Key); err != nil {
		return err
	}
	return c.compileExpr(kv.Value)
}

func (c *compiler) compileMapLit(expr *ast.CompositeLit) error {
	for _, elt := range expr.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return fmt.Errorf("element must be key:value")
		}
		if ident, ok := kv.Key.(*ast.Ident); ok {
			c.loadConst(ident.Name)
		} else if err := c.compileExpr(kv.Key); err != nil {
			return err
		}
		if err := c.compileExpr(kv.Value); err != nil {
			return err
		}
	}
	c.emit(taivm.OpMakeMap.With(len(expr.Elts)))
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
		return c.compileSwitchStmt(s, "")

	case *ast.ForStmt:
		return c.compileForStmt(s, "")

	case *ast.RangeStmt:
		return c.compileRangeStmt(s, "")

	case *ast.BranchStmt:
		return c.compileBranchStmt(s)

	case *ast.EmptyStmt:
		return nil

	case *ast.LabeledStmt:
		label := s.Label.Name
		c.labels[label] = len(c.code)
		switch st := s.Stmt.(type) {
		case *ast.ForStmt:
			return c.compileForStmt(st, label)
		case *ast.RangeStmt:
			return c.compileRangeStmt(st, label)
		case *ast.SwitchStmt:
			return c.compileSwitchStmt(st, label)
		case *ast.TypeSwitchStmt:
			return c.compileTypeSwitchStmt(st, label)
		default:
			return c.compileStmt(s.Stmt)
		}

	case *ast.DeferStmt:
		return c.compileDeferStmt(s)

	case *ast.GoStmt:
		return fmt.Errorf("go statement not supported")

	case *ast.SelectStmt:
		return fmt.Errorf("select statement not supported")

	case *ast.SendStmt:
		return fmt.Errorf("send statement not supported")

	case *ast.TypeSwitchStmt:
		return c.compileTypeSwitchStmt(s, "")

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
		if len(c.resultNames) > 0 {
			for _, name := range c.resultNames {
				idx := c.addConst(name)
				c.emit(taivm.OpLoadVar.With(idx))
			}
			if len(c.resultNames) > 1 {
				c.emit(taivm.OpMakeTuple.With(len(c.resultNames)))
			}
		} else {
			c.loadConst(nil)
		}
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
		return c.compileMultiAssignFromSingleRHS(stmt)
	}
	if len(stmt.Rhs) == len(stmt.Lhs) {
		return c.compileMultiAssignFromMultiRHS(stmt)
	}
	return fmt.Errorf("assignment count mismatch: %d = %d", len(stmt.Lhs), len(stmt.Rhs))
}

func (c *compiler) compileMultiAssignFromSingleRHS(stmt *ast.AssignStmt) error {
	rhs := stmt.Rhs[0]
	if ta, ok := rhs.(*ast.TypeAssertExpr); ok && len(stmt.Lhs) == 2 {
		if err := c.compileExpr(ta.X); err != nil {
			return err
		}
		if err := c.compileExpr(ta.Type); err != nil {
			return err
		}
		c.emit(taivm.OpTypeAssertOk)
		c.emit(taivm.OpSwap)
	} else if ie, ok := rhs.(*ast.IndexExpr); ok && len(stmt.Lhs) == 2 {
		if err := c.compileExpr(ie.X); err != nil {
			return err
		}
		if err := c.compileExpr(ie.Index); err != nil {
			return err
		}
		c.emit(taivm.OpGetIndexOk)
		c.emit(taivm.OpSwap)
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
	return nil
}

func (c *compiler) compileMultiAssignFromMultiRHS(stmt *ast.AssignStmt) error {
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
	return nil
}

func (c *compiler) compileSingleAssign(lhs, rhs ast.Expr, tok token.Token) error {
	if err := c.compileExpr(rhs); err != nil {
		return err
	}
	return c.compileAssignFromStack(lhs, tok)
}

func (c *compiler) compileCompoundAssign(lhs, rhs ast.Expr, tok token.Token) error {
	switch e := lhs.(type) {
	case *ast.IndexExpr:
		return c.compileIndexCompoundAssign(e, rhs, tok)
	case *ast.SelectorExpr:
		return c.compileSelectorCompoundAssign(e, rhs, tok)
	case *ast.Ident:
		return c.compileIdentCompoundAssign(e, rhs, tok)
	default:
		return fmt.Errorf("compound assignment to %T not supported", lhs)
	}
}

func (c *compiler) compileIndexCompoundAssign(lhs *ast.IndexExpr, rhs ast.Expr, tok token.Token) error {
	if err := c.compileExpr(lhs.X); err != nil {
		return err
	}
	if err := c.compileExpr(lhs.Index); err != nil {
		return err
	}
	c.emit(taivm.OpDup2)
	c.emit(taivm.OpGetIndex)
	if err := c.compileExpr(rhs); err != nil {
		return err
	}
	if err := c.emitBinaryOp(tok); err != nil {
		return err
	}
	c.emit(taivm.OpSetIndex)
	return nil
}

func (c *compiler) compileSelectorCompoundAssign(lhs *ast.SelectorExpr, rhs ast.Expr, tok token.Token) error {
	if err := c.compileExpr(lhs.X); err != nil {
		return err
	}
	c.emit(taivm.OpDup)
	c.loadConst(lhs.Sel.Name)
	c.emit(taivm.OpGetAttr)
	if err := c.compileExpr(rhs); err != nil {
		return err
	}
	if err := c.emitBinaryOp(tok); err != nil {
		return err
	}
	c.loadConst(lhs.Sel.Name)
	c.emit(taivm.OpSwap)
	c.emit(taivm.OpSetAttr)
	return nil
}

func (c *compiler) compileIdentCompoundAssign(lhs *ast.Ident, rhs ast.Expr, tok token.Token) error {
	name := lhs.Name
	if idx, ok := c.locals[name]; ok {
		c.emit(taivm.OpGetLocal.With(idx))
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}
		c.emit(taivm.OpSetLocal.With(idx))
		return nil
	}
	idx := c.addConst(name)
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

func (c *compiler) compileSwitchStmt(stmt *ast.SwitchStmt, label string) error {
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
	c.enterLoop(label)
	defer c.leaveLoop()
	if stmt.Tag != nil {
		if err := c.compileExpr(stmt.Tag); err != nil {
			return err
		}
	} else {
		c.loadConst(true)
	}
	return c.compileSwitchClauses(stmt)
}

func (c *compiler) compileSwitchClauses(stmt *ast.SwitchStmt) error {
	bodyJumps, defaultIdx, err := c.compileSwitchChecks(stmt)
	if err != nil {
		return err
	}
	var endJumps []int
	var defaultJump int
	if defaultIdx != -1 {
		defaultJump = c.emitJump(taivm.OpJump)
	} else {
		endJumps = append(endJumps, c.emitJump(taivm.OpJump))
	}
	endJumps, err = c.compileSwitchBodies(stmt, bodyJumps, defaultIdx, defaultJump, endJumps)
	if err != nil {
		return err
	}
	for _, jump := range endJumps {
		c.patchJump(jump, len(c.code))
	}
	c.emit(taivm.OpPop)
	return nil
}

func (c *compiler) compileSwitchChecks(stmt *ast.SwitchStmt) ([][]int, int, error) {
	var bodyJumps [][]int
	defaultIdx := -1
	for i, clause := range stmt.Body.List {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			return nil, 0, fmt.Errorf("switch body must be case clause")
		}
		if len(cc.List) == 0 {
			defaultIdx = i
			bodyJumps = append(bodyJumps, nil)
			continue
		}
		var jumps []int
		for _, expr := range cc.List {
			c.emit(taivm.OpDup)
			if err := c.compileExpr(expr); err != nil {
				return nil, 0, err
			}
			c.emit(taivm.OpEq)
			skip := c.emitJump(taivm.OpJumpFalse)
			jumps = append(jumps, c.emitJump(taivm.OpJump))
			c.patchJump(skip, len(c.code))
		}
		bodyJumps = append(bodyJumps, jumps)
	}
	return bodyJumps, defaultIdx, nil
}

func (c *compiler) compileSwitchBodies(stmt *ast.SwitchStmt, bodyJumps [][]int, defaultIdx int, defaultJump int, endJumps []int) ([]int, error) {
	for i, clause := range stmt.Body.List {
		cc := clause.(*ast.CaseClause)
		target := len(c.code)
		if i == defaultIdx {
			c.patchJump(defaultJump, target)
		} else {
			for _, jump := range bodyJumps[i] {
				c.patchJump(jump, target)
			}
		}
		if err := c.compileBlockStmt(&ast.BlockStmt{List: cc.Body}); err != nil {
			return nil, err
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
	return endJumps, nil
}

func (c *compiler) compileTypeSwitchStmt(stmt *ast.TypeSwitchStmt, label string) error {
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
	c.enterLoop(label)
	defer c.leaveLoop()
	var xExpr ast.Expr
	var boundVar string
	if assign, ok := stmt.Assign.(*ast.AssignStmt); ok {
		boundVar = assign.Lhs[0].(*ast.Ident).Name
		xExpr = assign.Rhs[0].(*ast.TypeAssertExpr).X
	} else if expr, ok := stmt.Assign.(*ast.ExprStmt); ok {
		xExpr = expr.X.(*ast.TypeAssertExpr).X
	}
	if err := c.compileExpr(xExpr); err != nil {
		return err
	}
	valTmpIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(valTmpIdx))
	return c.compileTypeSwitchClauses(stmt, valTmpIdx, boundVar)
}

func (c *compiler) compileTypeSwitchClauses(stmt *ast.TypeSwitchStmt, valTmpIdx int, boundVar string) error {
	var endJumps []int
	var defaultCase *ast.CaseClause
	for _, clause := range stmt.Body.List {
		cc := clause.(*ast.CaseClause)
		if len(cc.List) == 0 {
			defaultCase = cc
			continue
		}
		var matchJumps []int
		for _, typeExpr := range cc.List {
			c.emit(taivm.OpLoadVar.With(valTmpIdx))
			if id, ok := typeExpr.(*ast.Ident); ok && id.Name == "nil" {
				c.loadConst(nil)
				c.emit(taivm.OpEq)
			} else {
				if err := c.compileExpr(typeExpr); err != nil {
					return err
				}
				c.emit(taivm.OpTypeAssertOk)
				c.emit(taivm.OpSwap)
				c.emit(taivm.OpPop) // Pop result, keep ok
			}
			skipMatch := c.emitJump(taivm.OpJumpFalse)
			matchJumps = append(matchJumps, c.emitJump(taivm.OpJump))
			c.patchJump(skipMatch, len(c.code))
		}

		// If no types in this case matched, jump to next case
		nextClauseJump := c.emitJump(taivm.OpJump)

		bodyTarget := len(c.code)
		for _, jump := range matchJumps {
			c.patchJump(jump, bodyTarget)
		}

		if boundVar != "" {
			c.emit(taivm.OpLoadVar.With(valTmpIdx))
			c.emit(taivm.OpDefVar.With(c.addConst(boundVar)))
		}
		if err := c.compileBlockStmt(&ast.BlockStmt{List: cc.Body}); err != nil {
			return err
		}
		endJumps = append(endJumps, c.emitJump(taivm.OpJump))
		c.patchJump(nextClauseJump, len(c.code))
	}
	if defaultCase != nil {
		if boundVar != "" {
			c.emit(taivm.OpLoadVar.With(valTmpIdx))
			c.emit(taivm.OpDefVar.With(c.addConst(boundVar)))
		}
		if err := c.compileBlockStmt(&ast.BlockStmt{List: defaultCase.Body}); err != nil {
			return err
		}
	}
	for _, jump := range endJumps {
		c.patchJump(jump, len(c.code))
	}
	return nil
}

func (c *compiler) compileForStmt(stmt *ast.ForStmt, label string) error {
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

	loop := c.enterLoop(label)
	loop.isLoop = true
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

	c.emitJump(taivm.OpJump.With(loop.startPos - len(c.code) - 1))

	c.leaveLoop()
	return nil
}

func (c *compiler) compileRangeStmt(stmt *ast.RangeStmt, label string) error {
	if err := c.compileExpr(stmt.X); err != nil {
		return err
	}
	c.emit(taivm.OpGetIter)
	c.emit(taivm.OpDup)
	containerIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(containerIdx))
	c.emit(taivm.OpLoadVar.With(containerIdx))
	c.emit(taivm.OpEnterScope)
	c.scopeDepth++
	defer func() {
		c.emit(taivm.OpLeaveScope)
		c.scopeDepth--
	}()
	return c.compileRangeLoop(stmt, containerIdx, label)
}

func (c *compiler) compileRangeLoop(stmt *ast.RangeStmt, containerIdx int, label string) error {
	loop := c.enterLoop(label)
	loop.isRange = true
	loop.isLoop = true
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
		return c.compileGoto(stmt)
	}
	if stmt.Tok == token.FALLTHROUGH {
		return nil
	}
	targetIdx := -1
	if stmt.Label != nil {
		for i := len(c.loops) - 1; i >= 0; i-- {
			if c.loops[i].label == stmt.Label.Name {
				targetIdx = i
				break
			}
		}
		if targetIdx == -1 {
			return fmt.Errorf("label %s not found", stmt.Label.Name)
		}
	} else {
		targetIdx = len(c.loops) - 1
	}
	if targetIdx == -1 {
		return fmt.Errorf("branch statement outside loop or switch")
	}
	scope := c.loops[targetIdx]
	if stmt.Tok == token.CONTINUE && !scope.isLoop {
		return fmt.Errorf("continue label %s not on loop", stmt.Label.Name)
	}
	for i := len(c.loops) - 1; i >= targetIdx; i-- {
		if c.loops[i].isRange {
			c.emit(taivm.OpPop)
		}
	}
	for range c.scopeDepth - scope.entryDepth {
		c.emit(taivm.OpLeaveScope)
	}
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

func (c *compiler) compileGoto(stmt *ast.BranchStmt) error {
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

func (c *compiler) compileDeferStmt(stmt *ast.DeferStmt) error {
	sub := &compiler{
		name:         "defer",
		isFunc:       true,
		consts:       make(map[any]int),
		locals:       make(map[string]int),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		generics:     c.generics,
		typeParams:   c.typeParams,
		structFields: c.structFields,
		types:        c.types,
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
	fn.NumLocals = len(sub.locals)
	idx := c.addConst(fn)
	c.emit(taivm.OpMakeClosure.With(idx))
	c.emit(taivm.OpDefer)
	return nil
}

func (c *compiler) compileGenDecl(decl *ast.GenDecl) error {
	c.lastConstExprs = nil
	for i, spec := range decl.Specs {
		switch s := spec.(type) {
		case *ast.ImportSpec:
			if err := c.compileImportSpec(s); err != nil {
				return err
			}
		case *ast.ValueSpec:
			if decl.Tok == token.CONST {
				c.iotaVal = i
			}
			if err := c.compileValueSpec(s, decl.Tok == token.CONST); err != nil {
				return err
			}
		case *ast.TypeSpec:
			if err := c.compileTypeSpec(s); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *compiler) compileImportSpec(spec *ast.ImportSpec) error {
	path, err := strconv.Unquote(spec.Path.Value)
	if err != nil {
		return err
	}
	c.emit(taivm.OpImport.With(c.addConst(path)))
	c.emit(taivm.OpPop)
	return nil
}

func (c *compiler) compileValueSpec(s *ast.ValueSpec, isConst bool) error {
	values := s.Values
	if isConst && len(values) == 0 {
		values = c.lastConstExprs
	} else if isConst {
		c.lastConstExprs = values
	}
	if len(values) == 1 && len(s.Names) > 1 {
		return c.compileValueSpecSingleRHS(s, values[0])
	}
	if len(values) > 0 && len(values) != len(s.Names) {
		return fmt.Errorf("assignment count mismatch: %d = %d", len(s.Names), len(values))
	}
	for i, name := range s.Names {
		valExpr := any(nil)
		if i < len(values) {
			valExpr = values[i]
		}
		if err := c.compileNamedValueDef(name.Name, valExpr); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileNamedValueDef(name string, valExpr any) error {
	if name == "_" {
		if e, ok := valExpr.(ast.Expr); ok {
			if err := c.compileExpr(e); err != nil {
				return err
			}
		} else {
			c.loadConst(nil)
		}
		c.emit(taivm.OpPop)
		return nil
	}
	if e, ok := valExpr.(ast.Expr); ok {
		if err := c.compileExpr(e); err != nil {
			return err
		}
	} else {
		c.loadConst(nil)
	}
	c.emit(taivm.OpDefVar.With(c.addConst(name)))
	return nil
}

func (c *compiler) compileValueSpecSingleRHS(s *ast.ValueSpec, rhs ast.Expr) error {
	if ta, ok := rhs.(*ast.TypeAssertExpr); ok && len(s.Names) == 2 {
		if err := c.compileExpr(ta.X); err != nil {
			return err
		}
		if err := c.compileExpr(ta.Type); err != nil {
			return err
		}
		c.emit(taivm.OpTypeAssertOk)
		c.emit(taivm.OpSwap)
	} else if ie, ok := rhs.(*ast.IndexExpr); ok && len(s.Names) == 2 {
		if err := c.compileExpr(ie.X); err != nil {
			return err
		}
		if err := c.compileExpr(ie.Index); err != nil {
			return err
		}
		c.emit(taivm.OpGetIndexOk)
		c.emit(taivm.OpSwap)
	} else {
		if err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpUnpack.With(len(s.Names)))
	}
	for _, name := range s.Names {
		if name.Name == "_" {
			c.emit(taivm.OpPop)
			continue
		}
		c.emit(taivm.OpDefVar.With(c.addConst(name.Name)))
	}
	return nil
}

func (c *compiler) compileTypeSpec(spec *ast.TypeSpec) error {
	if spec.TypeParams != nil && len(spec.TypeParams.List) > 0 {
		if c.generics == nil {
			c.generics = make(map[string]bool)
		}
		c.generics[spec.Name.Name] = true
	}
	sub := c
	if spec.TypeParams != nil && len(spec.TypeParams.List) > 0 {
		sub = &compiler{
			name:         spec.Name.Name,
			consts:       make(map[any]int),
			generics:     c.generics,
			typeParams:   make(map[string]bool),
			structFields: c.structFields,
			types:        c.types,
		}
		for _, field := range spec.TypeParams.List {
			for _, name := range field.Names {
				sub.typeParams[name.Name] = true
			}
		}
	}
	t, err := sub.resolveType(spec.Type)
	if err != nil {
		return err
	}
	t.Name = spec.Name.Name
	c.types[spec.Name.Name] = t
	c.loadConst(t)
	c.emit(taivm.OpDefVar.With(c.addConst(spec.Name.Name)))
	if st, ok := spec.Type.(*ast.StructType); ok {
		var fields []string
		for _, f := range st.Fields.List {
			for _, n := range f.Names {
				fields = append(fields, n.Name)
			}
			if len(f.Names) == 0 {
				switch ft := f.Type.(type) {
				case *ast.Ident:
					fields = append(fields, ft.Name)
				case *ast.StarExpr:
					if id, ok := ft.X.(*ast.Ident); ok {
						fields = append(fields, id.Name)
					}
				}
			}
		}
		c.structFields[spec.Name.Name] = fields
		c.recordEmbeddedInfo(spec.Name.Name, st)
	}
	return nil
}

func (c *compiler) recordEmbeddedInfo(typeName string, st *ast.StructType) {
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
			case *ast.IndexExpr:
				if id, ok := ft.X.(*ast.Ident); ok {
					embedded = append(embedded, id.Name)
				}
			case *ast.IndexListExpr:
				if id, ok := ft.X.(*ast.Ident); ok {
					embedded = append(embedded, id.Name)
				}
			}
		}
	}
	if len(embedded) > 0 {
		for _, e := range embedded {
			c.loadConst(e)
		}
		c.emit(taivm.OpMakeList.With(len(embedded)))
		c.emit(taivm.OpDefVar.With(c.addConst("_embedded_info_" + typeName)))
	}
}

func (c *compiler) resolveType(expr ast.Expr) (*taivm.Type, error) {
	if expr == nil {
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return c.resolveIdentType(e)
	case *ast.ArrayType:
		return c.resolveArrayType(e)
	case *ast.Ellipsis:
		return c.resolveEllipsisType(e)
	case *ast.ChanType:
		return c.resolveChanType(e)
	case *ast.FuncType:
		return c.resolveFuncType(e)
	case *ast.MapType:
		return c.resolveMapType(e)
	case *ast.StarExpr:
		return c.resolveStarType(e)
	case *ast.StructType:
		return c.resolveStructType(e)
	case *ast.InterfaceType:
		return c.resolveInterfaceType(e)
	case *ast.SelectorExpr:
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	case *ast.IndexExpr:
		return c.resolveType(e.X)
	case *ast.IndexListExpr:
		return c.resolveType(e.X)
	case *ast.ParenExpr:
		return c.resolveType(e.X)
	default:
		return nil, fmt.Errorf("unsupported type expression: %T", expr)
	}
}

func (c *compiler) resolveArrayType(e *ast.ArrayType) (*taivm.Type, error) {
	elt, err := c.resolveType(e.Elt)
	if err != nil {
		return nil, err
	}
	if e.Len != nil {
		var length int
		if lit, ok := e.Len.(*ast.BasicLit); ok && lit.Kind == token.INT {
			i, _ := strconv.Atoi(lit.Value)
			length = i
		}
		return &taivm.Type{
			Kind: taivm.KindArray,
			Elem: elt,
			Len:  length,
		}, nil
	}
	return &taivm.Type{
		Kind: taivm.KindSlice,
		Elem: elt,
	}, nil
}

func (c *compiler) resolveEllipsisType(e *ast.Ellipsis) (*taivm.Type, error) {
	if e.Elt == nil {
		return taivm.FromReflectType(reflect.TypeFor[[]any]()), nil
	}
	elt, err := c.resolveType(e.Elt)
	if err != nil {
		return nil, err
	}
	return &taivm.Type{
		Kind: taivm.KindSlice,
		Elem: elt,
	}, nil
}

func (c *compiler) resolveChanType(e *ast.ChanType) (*taivm.Type, error) {
	t, err := c.resolveType(e.Value)
	if err != nil {
		return nil, err
	}
	kind := taivm.KindChan
	return &taivm.Type{
		Kind: kind,
		Elem: t,
	}, nil
}

func (c *compiler) resolveMapType(e *ast.MapType) (*taivm.Type, error) {
	kt, err := c.resolveType(e.Key)
	if err != nil {
		return nil, err
	}
	vt, err := c.resolveType(e.Value)
	if err != nil {
		return nil, err
	}
	return &taivm.Type{
		Kind: taivm.KindMap,
		Key:  kt,
		Elem: vt,
	}, nil
}

func (c *compiler) resolveStarType(e *ast.StarExpr) (*taivm.Type, error) {
	t, err := c.resolveType(e.X)
	if err != nil {
		return nil, err
	}
	return &taivm.Type{
		Kind: taivm.KindPtr,
		Elem: t,
	}, nil
}

func (c *compiler) resolveFuncType(e *ast.FuncType) (*taivm.Type, error) {
	params, err := c.resolveFuncFields(e.Params)
	if err != nil {
		return nil, err
	}
	results, err := c.resolveFuncFields(e.Results)
	if err != nil {
		return nil, err
	}
	variadic := false
	if e.Params != nil && len(e.Params.List) > 0 {
		last := e.Params.List[len(e.Params.List)-1]
		if _, ok := last.Type.(*ast.Ellipsis); ok {
			variadic = true
		}
	}
	return &taivm.Type{
		Kind:     taivm.KindFunc,
		In:       params,
		Out:      results,
		Variadic: variadic,
	}, nil
}

func (c *compiler) resolveFuncFields(fields *ast.FieldList) ([]*taivm.Type, error) {
	if fields == nil {
		return nil, nil
	}
	var res []*taivm.Type
	for _, field := range fields.List {
		t, err := c.resolveType(field.Type)
		if err != nil {
			return nil, err
		}
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			res = append(res, t)
		}
	}
	return res, nil
}

func (c *compiler) resolveStructType(e *ast.StructType) (*taivm.Type, error) {
	res := &taivm.Type{Kind: taivm.KindStruct}
	if e.Fields != nil {
		for _, field := range e.Fields.List {
			t, err := c.resolveType(field.Type)
			if err != nil {
				return nil, err
			}
			var tag string
			if field.Tag != nil {
				tg, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					return nil, err
				}
				tag = tg
			}
			if len(field.Names) == 0 {
				name := ""
				switch ft := field.Type.(type) {
				case *ast.Ident:
					name = ft.Name
				case *ast.StarExpr:
					if id, ok := ft.X.(*ast.Ident); ok {
						name = id.Name
					}
				case *ast.IndexExpr:
					if id, ok := ft.X.(*ast.Ident); ok {
						name = id.Name
					}
				case *ast.IndexListExpr:
					if id, ok := ft.X.(*ast.Ident); ok {
						name = id.Name
					}
				}
				res.Fields = append(res.Fields, taivm.StructField{
					Name:      name,
					Type:      t,
					Tag:       tag,
					Anonymous: true,
				})
			}
			for _, name := range field.Names {
				res.Fields = append(res.Fields, taivm.StructField{
					Name:      name.Name,
					Type:      t,
					Tag:       tag,
					Anonymous: false,
				})
			}
		}
	}
	return res, nil
}

func (c *compiler) resolveIdentType(e *ast.Ident) (*taivm.Type, error) {
	if c.typeParams != nil && c.typeParams[e.Name] {
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	}
	if t, ok := c.types[e.Name]; ok {
		return t, nil
	}
	switch e.Name {
	case "int":
		return taivm.FromReflectType(reflect.TypeFor[int]()), nil
	case "int8":
		return taivm.FromReflectType(reflect.TypeFor[int8]()), nil
	case "int16":
		return taivm.FromReflectType(reflect.TypeFor[int16]()), nil
	case "int32", "rune":
		return taivm.FromReflectType(reflect.TypeFor[int32]()), nil
	case "int64":
		return taivm.FromReflectType(reflect.TypeFor[int64]()), nil
	case "uint":
		return taivm.FromReflectType(reflect.TypeFor[uint]()), nil
	case "uint8", "byte":
		return taivm.FromReflectType(reflect.TypeFor[uint8]()), nil
	case "uint16":
		return taivm.FromReflectType(reflect.TypeFor[uint16]()), nil
	case "uint32":
		return taivm.FromReflectType(reflect.TypeFor[uint32]()), nil
	case "uint64":
		return taivm.FromReflectType(reflect.TypeFor[uint64]()), nil
	case "float32":
		return taivm.FromReflectType(reflect.TypeFor[float32]()), nil
	case "float64":
		return taivm.FromReflectType(reflect.TypeFor[float64]()), nil
	case "string":
		return taivm.FromReflectType(reflect.TypeFor[string]()), nil
	case "any":
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	case "error":
		return taivm.FromReflectType(reflect.TypeFor[error]()), nil
	case "bool":
		return taivm.FromReflectType(reflect.TypeFor[bool]()), nil
	case "complex64":
		return taivm.FromReflectType(reflect.TypeFor[complex64]()), nil
	case "complex128":
		return taivm.FromReflectType(reflect.TypeFor[complex128]()), nil
	case "comparable":
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	default:
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil
	}
}

func (c *compiler) resolveInterfaceType(e *ast.InterfaceType) (*taivm.Type, error) {
	methods := make(map[string]*taivm.Type)
	if e.Methods != nil {
		for _, field := range e.Methods.List {
			t, err := c.resolveType(field.Type)
			if err != nil {
				return nil, err
			}
			for _, name := range field.Names {
				methods[name.Name] = t
			}
		}
	}
	return &taivm.Type{
		Kind:    taivm.KindInterface,
		Methods: methods,
	}, nil
}

func (c *compiler) compileAssignFromStack(lhs ast.Expr, tok token.Token) error {
	switch e := lhs.(type) {
	case *ast.Ident:
		return c.compileIdentAssign(e, tok)
	case *ast.IndexExpr:
		return c.compileIndexAssign(e)
	case *ast.SelectorExpr:
		return c.compileSelectorAssign(e)
	case *ast.StarExpr:
		return c.compileDerefAssign(e)
	default:
		return fmt.Errorf("assignment to %T not supported", lhs)
	}
}

func (c *compiler) compileIdentAssign(e *ast.Ident, tok token.Token) error {
	name := e.Name
	if name == "_" {
		c.emit(taivm.OpPop)
		return nil
	}
	if idx, ok := c.locals[name]; ok {
		c.emit(taivm.OpSetLocal.With(idx))
		return nil
	}
	if tok == token.DEFINE && c.isFunc {
		idx := len(c.locals)
		c.locals[name] = idx
		c.emit(taivm.OpSetLocal.With(idx))
		return nil
	}
	idx := c.addConst(name)
	if tok == token.DEFINE {
		c.emit(taivm.OpDefVar.With(idx))
	} else {
		c.emit(taivm.OpSetVar.With(idx))
	}
	return nil
}

func (c *compiler) compileIndexAssign(e *ast.IndexExpr) error {
	tmpIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(tmpIdx))
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	if err := c.compileExpr(e.Index); err != nil {
		return err
	}
	c.emit(taivm.OpLoadVar.With(tmpIdx))
	c.emit(taivm.OpSetIndex)
	return nil
}

func (c *compiler) compileSelectorAssign(e *ast.SelectorExpr) error {
	tmpIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(tmpIdx))
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	c.loadConst(e.Sel.Name)
	c.emit(taivm.OpLoadVar.With(tmpIdx))
	c.emit(taivm.OpSetAttr)
	return nil
}

func (c *compiler) compileDerefAssign(e *ast.StarExpr) error {
	if err := c.compileExpr(e.X); err != nil {
		return err
	}
	c.emit(taivm.OpSwap)
	c.emit(taivm.OpSetDeref)
	return nil
}
