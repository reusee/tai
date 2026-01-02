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
	locals         map[string]variable
	iotaVal        int
	labels         map[string]int
	unresolved     map[string][]int
	lastConstExprs []ast.Expr
	resultNames    []string
	generics       map[string]bool // Top-level generic names
	typeParams     map[string]bool // Current function/type parameters
	structFields   map[string][]string
	types          map[string]*taivm.Type
	globals        map[string]*taivm.Type
}

type variable struct {
	index int
	typ   *taivm.Type
}

var basicTypes = map[string]reflect.Type{
	"int":        reflect.TypeFor[int](),
	"int8":       reflect.TypeFor[int8](),
	"int16":      reflect.TypeFor[int16](),
	"int32":      reflect.TypeFor[int32](),
	"rune":       reflect.TypeFor[int32](),
	"int64":      reflect.TypeFor[int64](),
	"uint":       reflect.TypeFor[uint](),
	"uint8":      reflect.TypeFor[uint8](),
	"byte":       reflect.TypeFor[uint8](),
	"uint16":     reflect.TypeFor[uint16](),
	"uint32":     reflect.TypeFor[uint32](),
	"uint64":     reflect.TypeFor[uint64](),
	"float32":    reflect.TypeFor[float32](),
	"float64":    reflect.TypeFor[float64](),
	"string":     reflect.TypeFor[string](),
	"any":        reflect.TypeFor[any](),
	"error":      reflect.TypeFor[error](),
	"bool":       reflect.TypeFor[bool](),
	"complex64":  reflect.TypeFor[complex64](),
	"complex128": reflect.TypeFor[complex128](),
	"comparable": reflect.TypeFor[any](),
}

func newCompiler() *compiler {
	return &compiler{
		consts:       make(map[any]int),
		locals:       make(map[string]variable),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		structFields: make(map[string][]string),
		types:        make(map[string]*taivm.Type),
		globals:      make(map[string]*taivm.Type),
	}
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

func (c *compiler) compileFiles(files []*ast.File) error {
	if len(files) == 0 {
		return nil
	}
	c.name = files[0].Name.Name
	symbols, err := c.collectSymbols(files)
	if err != nil {
		return err
	}
	inits, decls, hasMain, err := c.collectDecls(files, symbols)
	if err != nil {
		return err
	}
	sorted, err := sortTopDecls(decls)
	if err != nil {
		return err
	}
	for _, d := range sorted {
		if err := d.compile(); err != nil {
			return err
		}
	}
	for _, initFn := range inits {
		if err := initFn(); err != nil {
			return err
		}
	}
	if hasMain {
		c.emit(taivm.OpLoadVar.With(c.addConst("main")))
		c.emit(taivm.OpCall.With(0))
		c.emit(taivm.OpPop)
	}
	return nil
}

func (c *compiler) collectSymbols(files []*ast.File) (map[string]bool, error) {
	symbols := make(map[string]bool)
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name != "init" {
					name := d.Name.Name
					if name == "_" {
						continue
					}
					if symbols[name] {
						return nil, fmt.Errorf("%s redeclared in this block", name)
					}
					symbols[name] = true
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.Name == "_" {
								continue
							}
							if symbols[name.Name] {
								return nil, fmt.Errorf("%s redeclared in this block", name.Name)
							}
							symbols[name.Name] = true
						}
					case *ast.TypeSpec:
						name := s.Name.Name
						if name == "_" {
							continue
						}
						if symbols[name] {
							return nil, fmt.Errorf("%s redeclared in this block", name)
						}
						symbols[name] = true
					}
				}
			}
		}
	}
	return symbols, nil
}

func (c *compiler) collectDecls(files []*ast.File, symbols map[string]bool) ([]func() error, []topDecl, bool, error) {
	var decls []topDecl
	var inits []func() error
	var hasMain bool
	getDeps := func(node ast.Node) []string {
		var deps []string
		ast.Inspect(node, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && symbols[id.Name] {
				deps = append(deps, id.Name)
			}
			return true
		})
		return deps
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			if err := c.processTopDecl(decl, getDeps, &inits, &decls, &hasMain); err != nil {
				return nil, nil, false, err
			}
		}
	}
	return inits, decls, hasMain, nil
}

func (c *compiler) processTopDecl(decl ast.Decl, getDeps func(ast.Node) []string, inits *[]func() error, decls *[]topDecl, hasMain *bool) error {
	switch g := decl.(type) {
	case *ast.FuncDecl:
		if g.Recv == nil && g.Name.Name == "init" {
			*inits = append(*inits, func() error { return c.compileInitFunc(g) })
		} else {
			if g.Name.Name == "main" {
				*hasMain = true
			}
			deps := getDeps(g)
			*decls = append(*decls, topDecl{
				names:   []string{g.Name.Name},
				deps:    deps,
				compile: func() error { return c.compileFuncDecl(g) },
			})
		}
	case *ast.GenDecl:
		if g.Tok == token.IMPORT {
			return c.compileGenDecl(g)
		}
		c.processGenDecl(g, getDeps, decls)
	}
	return nil
}

func (c *compiler) processGenDecl(g *ast.GenDecl, getDeps func(ast.Node) []string, vars *[]topDecl) {
	var lastExprs []ast.Expr
	for i, spec := range g.Specs {
		var names []string
		var deps []string
		switch s := spec.(type) {
		case *ast.ValueSpec:
			for _, n := range s.Names {
				names = append(names, n.Name)
			}
			vals := s.Values
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
			names = append(names, s.Name.Name)
			deps = append(deps, getDeps(s.Type)...)
		}
		capturedExprs, capturedIota, capturedSpec := lastExprs, i, spec
		*vars = append(*vars, topDecl{
			names: names, deps: deps,
			compile: func() error {
				c.iotaVal, c.lastConstExprs = capturedIota, capturedExprs
				if v, ok := capturedSpec.(*ast.ValueSpec); ok {
					return c.compileValueSpec(v, g.Tok == token.CONST)
				}
				return c.compileTypeSpec(capturedSpec.(*ast.TypeSpec))
			},
		})
	}
}

func (c *compiler) getFunction() *taivm.Function {
	return &taivm.Function{
		Name:      c.name,
		Code:      c.code,
		Constants: c.constants,
	}
}

func (c *compiler) initExternal(externalTypes, externalValueTypes map[string]*taivm.Type) {
	for name, t := range externalTypes {
		c.types[name] = t
		st := t
		if st.Kind == taivm.KindExternal {
			rt := st.External
			if rt.Kind() == reflect.Pointer {
				rt = rt.Elem()
			}
			if rt.Kind() == reflect.Struct {
				var fields []string
				for i := 0; i < rt.NumField(); i++ {
					fields = append(fields, rt.Field(i).Name)
				}
				c.structFields[name] = fields
			}
			continue
		}
		if st.Kind == taivm.KindPtr && st.Elem != nil {
			st = st.Elem
		}
		if st.Kind == taivm.KindStruct {
			var fields []string
			for _, f := range st.Fields {
				fields = append(fields, f.Name)
			}
			c.structFields[name] = fields
		}
	}
	for name, t := range externalValueTypes {
		c.globals[name] = t
	}
}

func (c *compiler) getPackage() *Package {
	return &Package{
		Name: c.name,
		Init: c.getFunction(),
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

func (c *compiler) typeOf(val any) *taivm.Type {
	if val == nil {
		return nil
	}
	return taivm.FromReflectType(reflect.TypeOf(val))
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
		t := recv.Type
		if star, ok := t.(*ast.StarExpr); ok {
			isPointer = true
			t = star.X
		}
		typeName, _ = c.extractTypeName(t)
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

func (c *compiler) extractTypeName(t ast.Expr) (string, string) {
	if t == nil {
		return "", ""
	}
	switch e := t.(type) {
	case *ast.Ident:
		return e.Name, e.Name
	case *ast.SelectorExpr:
		prefix, _ := c.extractTypeName(e.X)
		if prefix != "" {
			return prefix + "." + e.Sel.Name, e.Sel.Name
		}
		return e.Sel.Name, e.Sel.Name
	case *ast.IndexExpr:
		return c.extractTypeName(e.X)
	case *ast.IndexListExpr:
		return c.extractTypeName(e.X)
	case *ast.StarExpr:
		return c.extractTypeName(e.X)
	case *ast.ParenExpr:
		return c.extractTypeName(e.X)
	}
	return "", ""
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
	typeParams := make(map[string]bool)
	if decl.Type.TypeParams != nil {
		for _, field := range decl.Type.TypeParams.List {
			for _, name := range field.Names {
				typeParams[name.Name] = true
			}
		}
	}
	fn, err := c.compileFunctionBody(decl.Name.Name, decl.Recv, decl.Type, decl.Body, typeParams)
	if err != nil {
		return nil, err
	}
	if err := c.setFuncMetadata(fn, decl.Recv, decl.Type); err != nil {
		return nil, err
	}
	return fn, nil
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

func (c *compiler) compileFunctionBody(name string, recv *ast.FieldList, fType *ast.FuncType, body *ast.BlockStmt, typeParams map[string]bool) (*taivm.Function, error) {
	sub := newCompiler()
	sub.name = name
	sub.isFunc = true
	sub.generics = c.generics
	sub.typeParams = typeParams
	sub.structFields = c.structFields
	sub.types = c.types
	sub.globals = c.globals

	if recv != nil && len(recv.List) > 0 {
		recvField := recv.List[0]
		if _, err := sub.resolveType(recvField.Type); err != nil {
			return nil, err
		}
	}
	if fType.Params != nil {
		for _, field := range fType.Params.List {
			if _, err := sub.resolveType(field.Type); err != nil {
				return nil, err
			}
		}
	}

	sub.setupResults(fType)
	if err := sub.defineNamedResults(fType); err != nil {
		return nil, err
	}
	for _, stmt := range body.List {
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
	fn.NumLocals = len(sub.locals)
	return fn, nil
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

func (c *compiler) setFuncMetadata(fn *taivm.Function, recv *ast.FieldList, fType *ast.FuncType) error {
	ft, err := c.resolveType(fType)
	if err != nil {
		return err
	}
	if recv != nil && len(recv.List) > 0 {
		rField := recv.List[0]
		rt, err := c.resolveType(rField.Type)
		if err != nil {
			return err
		}
		ft.In = append([]*taivm.Type{rt}, ft.In...)
	}
	fn.Type = ft
	fn.ParamNames = c.getFuncParamNames(recv, fType)
	fn.NumParams = len(fn.ParamNames)
	if fType.Params != nil {
		for _, field := range fType.Params.List {
			if _, ok := field.Type.(*ast.Ellipsis); ok {
				fn.Variadic = true
			}
		}
	}
	return nil
}

func (c *compiler) getFuncParamNames(recv *ast.FieldList, fType *ast.FuncType) []string {
	var names []string
	if recv != nil && len(recv.List) > 0 {
		rField := recv.List[0]
		for _, name := range rField.Names {
			names = append(names, name.Name)
		}
		if len(rField.Names) == 0 {
			names = append(names, "")
		}
	}
	if fType.Params != nil {
		for _, field := range fType.Params.List {
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

func (c *compiler) compileExpr(expr ast.Expr) (*taivm.Type, error) {
	if val, ok := c.evalConst(expr); ok {
		c.loadConst(val)
		return c.typeOf(val), nil
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
		return nil, fmt.Errorf("%T expression not supported here", expr)
	case *ast.BadExpr:
		return nil, fmt.Errorf("bad expression")
	default:
		return nil, fmt.Errorf("unknown expr type: %T", expr)
	}
}

func (c *compiler) compileStarExpr(expr *ast.StarExpr) (*taivm.Type, error) {
	t, err := c.compileExpr(expr.X)
	if err != nil {
		return nil, err
	}
	c.emit(taivm.OpDeref)
	if t != nil && t.Kind == taivm.KindPtr {
		return t.Elem, nil
	}
	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
}

func (c *compiler) compileTypeAssertExpr(expr *ast.TypeAssertExpr) (*taivm.Type, error) {
	if _, err := c.compileExpr(expr.X); err != nil {
		return nil, err
	}
	if expr.Type == nil {
		return nil, fmt.Errorf("type switch not supported")
	}
	t, err := c.resolveType(expr.Type)
	if err != nil {
		return nil, err
	}
	c.loadConst(t)
	c.emit(taivm.OpTypeAssert)
	return t, nil
}

func (c *compiler) compileTypeExpr(expr ast.Expr) (*taivm.Type, error) {
	t, err := c.resolveType(expr)
	if err != nil {
		return nil, err
	}
	c.loadConst(t)
	return taivm.FromReflectType(reflect.TypeFor[*taivm.Type]()), nil
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

func (c *compiler) compileIdentifier(expr *ast.Ident) (*taivm.Type, error) {
	name := expr.Name
	switch name {
	case "iota":
		c.loadConst(int64(c.iotaVal))
		return taivm.FromReflectType(reflect.TypeFor[int]()), nil
	case "true", "false":
		c.loadConst(name == "true")
		return taivm.FromReflectType(reflect.TypeFor[bool]()), nil
	case "nil":
		c.loadConst(nil)
		return nil, nil
	}
	if v, ok := c.locals[name]; ok {
		c.emit(taivm.OpGetLocal.With(v.index))
		return v.typ, nil
	}
	switch name {
	case "int", "int8", "int16", "int32", "rune", "int64",
		"uint", "uint8", "byte", "uint16", "uint32", "uint64",
		"float32", "float64", "string", "any", "error", "bool",
		"complex64", "complex128":
		t, err := c.resolveType(expr)
		if err != nil {
			return nil, err
		}
		c.loadConst(t)
		return taivm.FromReflectType(reflect.TypeFor[*taivm.Type]()), nil
	}
	idx := c.addConst(name)
	c.emit(taivm.OpLoadVar.With(idx))
	if t, ok := c.globals[name]; ok {
		return t, nil
	}
	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
}

func (c *compiler) compileBinaryExpr(expr *ast.BinaryExpr) (*taivm.Type, error) {
	if expr.Op == token.LAND {
		return c.compileLogicAnd(expr)
	}
	if expr.Op == token.LOR {
		return c.compileLogicOr(expr)
	}

	leftType, err := c.compileExpr(expr.X)
	if err != nil {
		return nil, err
	}
	rightType, err := c.compileExpr(expr.Y)
	if err != nil {
		return nil, err
	}

	// Basic type checking
	if leftType != nil && rightType != nil {
		// TODO: stricter checks based on operator
	}

	if err := c.emitBinaryOp(expr.Op); err != nil {
		return nil, err
	}

	switch expr.Op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return taivm.FromReflectType(reflect.TypeFor[bool]()), nil
	}
	return leftType, nil
}

func (c *compiler) compileLogicAnd(expr *ast.BinaryExpr) (*taivm.Type, error) {
	// a && b
	if _, err := c.compileExpr(expr.X); err != nil {
		return nil, err
	}
	// Stack: [a]
	c.emit(taivm.OpDup)
	// Stack: [a, a]
	jumpEnd := c.emitJump(taivm.OpJumpFalse)

	// Fallthrough: a is true. Result is result of b.
	c.emit(taivm.OpPop) // Pop a
	if _, err := c.compileExpr(expr.Y); err != nil {
		return nil, err
	}

	c.patchJump(jumpEnd, len(c.code))
	return taivm.FromReflectType(reflect.TypeFor[bool]()), nil
}

func (c *compiler) compileLogicOr(expr *ast.BinaryExpr) (*taivm.Type, error) {
	// a || b
	if _, err := c.compileExpr(expr.X); err != nil {
		return nil, err
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
	if _, err := c.compileExpr(expr.Y); err != nil {
		return nil, err
	}

	c.patchJump(jumpEnd, len(c.code))
	return taivm.FromReflectType(reflect.TypeFor[bool]()), nil
}

func (c *compiler) compileUnaryExpr(expr *ast.UnaryExpr) (*taivm.Type, error) {
	switch expr.Op {

	case token.SUB:
		c.loadConst(0)
		t, err := c.compileExpr(expr.X)
		if err != nil {
			return nil, err
		}
		c.emit(taivm.OpSub)
		return t, nil

	case token.ADD:
		return c.compileExpr(expr.X)

	case token.AND:
		switch x := expr.X.(type) {
		case *ast.Ident:
			idx := c.addConst(x.Name)
			c.emit(taivm.OpAddrOf.With(idx))
			if v, ok := c.locals[x.Name]; ok && v.typ != nil {
				return &taivm.Type{Kind: taivm.KindPtr, Elem: v.typ}, nil
			}

		case *ast.IndexExpr:
			if _, err := c.compileExpr(x.X); err != nil {
				return nil, err
			}
			if _, err := c.compileExpr(x.Index); err != nil {
				return nil, err
			}
			c.emit(taivm.OpAddrOfIndex)

		case *ast.SelectorExpr:
			if _, err := c.compileExpr(x.X); err != nil {
				return nil, err
			}
			c.loadConst(x.Sel.Name)
			c.emit(taivm.OpAddrOfAttr)

		case *ast.StarExpr:
			if _, err := c.compileExpr(x.X); err != nil {
				return nil, err
			}
			c.emit(taivm.OpDeref)
			tmpIdx := c.addConst(c.nextTmp())
			c.emit(taivm.OpDefVar.With(tmpIdx))
			c.emit(taivm.OpAddrOf.With(tmpIdx))

		case *ast.CompositeLit:
			if _, err := c.compileCompositeLit(x); err != nil {
				return nil, err
			}
			tmpIdx := c.addConst(c.nextTmp())
			c.emit(taivm.OpDefVar.With(tmpIdx))
			c.emit(taivm.OpAddrOf.With(tmpIdx))

		case *ast.ParenExpr:
			return c.compileUnaryExpr(&ast.UnaryExpr{Op: token.AND, X: x.X})

		default:
			return nil, fmt.Errorf("cannot take address of %T", expr.X)
		}
		// Fallback for ptr type unknown
		return taivm.FromReflectType(reflect.TypeFor[any]()), nil

	default:
		t, err := c.compileExpr(expr.X)
		if err != nil {
			return nil, err
		}
		switch expr.Op {

		case token.NOT:
			c.emit(taivm.OpNot)
			return taivm.FromReflectType(reflect.TypeFor[bool]()), nil

		case token.XOR:
			c.emit(taivm.OpBitNot)
			return t, nil

		default:
			return nil, fmt.Errorf("unknown unary operator: %s", expr.Op)

		}
	}
}

func (c *compiler) compileCallExpr(expr *ast.CallExpr) (*taivm.Type, error) {
	if sel, ok := expr.Fun.(*ast.SelectorExpr); ok {
		t, err := c.compileExpr(sel.X)
		if err != nil {
			return nil, err
		}

		isNamed := t != nil && t.Name != ""
		isNamedPtr := t != nil && t.Kind == taivm.KindPtr && t.Elem != nil && t.Elem.Name != ""

		if (isNamed || isNamedPtr) &&
			(t.Kind != taivm.KindStruct && t.Kind != taivm.KindInterface) &&
			!(isNamedPtr && t.Elem.Kind == taivm.KindStruct) {

			methodName := ""
			if isNamed {
				methodName = t.Name + "." + sel.Sel.Name
			} else {
				methodName = "*" + t.Elem.Name + "." + sel.Sel.Name
			}

			idx := c.addConst(methodName)
			c.emit(taivm.OpLoadVar.With(idx))
			c.emit(taivm.OpSwap)
			for _, arg := range expr.Args {
				if _, err := c.compileExpr(arg); err != nil {
					return nil, err
				}
			}
			if expr.Ellipsis != token.NoPos {
				numExplicit := len(expr.Args) - 1
				c.emit(taivm.OpMakeList.With(numExplicit))
				c.emit(taivm.OpAdd)
				c.emit(taivm.OpMakeMap.With(0))
				c.emit(taivm.OpCallKw)
			} else {
				c.emit(taivm.OpCall.With(len(expr.Args) + 1))
			}
			return taivm.FromReflectType(reflect.TypeFor[any]()), nil
		}
		c.loadConst(sel.Sel.Name)
		c.emit(taivm.OpGetAttr)
	} else {
		if _, err := c.compileExpr(expr.Fun); err != nil {
			return nil, err
		}
	}

	if expr.Ellipsis != token.NoPos {
		if err := c.compileVariadicCall(expr); err != nil {
			return nil, err
		}
	} else {
		for _, arg := range expr.Args {
			if _, err := c.compileExpr(arg); err != nil {
				return nil, err
			}
		}
		c.emit(taivm.OpCall.With(len(expr.Args)))
	}
	// TODO: infer return type from function signature
	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
}

func (c *compiler) compileVariadicCall(expr *ast.CallExpr) error {
	numExplicit := len(expr.Args) - 1
	for i := range numExplicit {
		if _, err := c.compileExpr(expr.Args[i]); err != nil {
			return err
		}
	}
	c.emit(taivm.OpMakeList.With(numExplicit))
	if _, err := c.compileExpr(expr.Args[numExplicit]); err != nil {
		return err
	}
	c.emit(taivm.OpAdd)
	c.emit(taivm.OpMakeMap.With(0))
	c.emit(taivm.OpCallKw)
	return nil
}

func (c *compiler) compileIndexExpr(expr *ast.IndexExpr) (*taivm.Type, error) {
	if id, ok := expr.X.(*ast.Ident); ok && c.generics != nil && c.generics[id.Name] {
		return c.compileExpr(expr.X)
	}
	t, err := c.compileExpr(expr.X)
	if err != nil {
		return nil, err
	}
	if _, err := c.compileExpr(expr.Index); err != nil {
		return nil, err
	}
	c.emit(taivm.OpGetIndex)

	if t != nil {
		if t.Kind == taivm.KindSlice || t.Kind == taivm.KindArray || t.Kind == taivm.KindMap || t.Kind == taivm.KindPtr { // Ptr to array
			// If ptr, assume ptr to array? Go allows it.
			if t.Kind == taivm.KindPtr && t.Elem != nil && t.Elem.Kind == taivm.KindArray {
				return t.Elem.Elem, nil
			}
			return t.Elem, nil
		}
	}

	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
}

func (c *compiler) compileIndexListExpr(expr *ast.IndexListExpr) (*taivm.Type, error) {
	if id, ok := expr.X.(*ast.Ident); ok && c.generics != nil && c.generics[id.Name] {
		return c.compileExpr(expr.X)
	}
	return c.compileExpr(expr.X)
}

func (c *compiler) compileSelectorExpr(expr *ast.SelectorExpr) (*taivm.Type, error) {
	typeName, _ := c.extractTypeName(expr)
	if t, ok := c.types[typeName]; ok {
		c.loadConst(t)
		return taivm.FromReflectType(reflect.TypeFor[*taivm.Type]()), nil
	}
	if t, ok := c.globals[typeName]; ok {
		c.emit(taivm.OpLoadVar.With(c.addConst(typeName)))
		return t, nil
	}

	t, err := c.compileExpr(expr.X)
	if err != nil {
		return nil, err
	}
	c.loadConst(expr.Sel.Name)
	c.emit(taivm.OpGetAttr)
	if t != nil && t.Kind == taivm.KindStruct {
		for _, f := range t.Fields {
			if f.Name == expr.Sel.Name {
				return f.Type, nil
			}
		}
	}
	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
}

func (c *compiler) compileSliceExpr(expr *ast.SliceExpr) (*taivm.Type, error) {
	t, err := c.compileExpr(expr.X)
	if err != nil {
		return nil, err
	}
	if expr.Low != nil {
		if _, err := c.compileExpr(expr.Low); err != nil {
			return nil, err
		}
	} else {
		c.loadConst(nil)
	}
	if expr.High != nil {
		if _, err := c.compileExpr(expr.High); err != nil {
			return nil, err
		}
	} else {
		c.loadConst(nil)
	}
	if expr.Slice3 {
		if expr.Max != nil {
			if _, err := c.compileExpr(expr.Max); err != nil {
				return nil, err
			}
		} else {
			c.loadConst(nil)
		}
	} else {
		c.loadConst(nil)
	}
	c.emit(taivm.OpGetSlice)
	return t, nil
}

func (c *compiler) compileFuncLit(expr *ast.FuncLit) (*taivm.Type, error) {
	typeParams := make(map[string]bool)
	maps.Copy(typeParams, c.typeParams)
	if expr.Type.TypeParams != nil {
		for _, field := range expr.Type.TypeParams.List {
			for _, name := range field.Names {
				typeParams[name.Name] = true
			}
		}
	}
	fn, err := c.compileFunctionBody("anon", nil, expr.Type, expr.Body, typeParams)
	if err != nil {
		return nil, err
	}
	if err := c.setFuncMetadata(fn, nil, expr.Type); err != nil {
		return nil, err
	}
	idx := c.addConst(fn)
	c.emit(taivm.OpMakeClosure.With(idx))
	return fn.Type, nil
}

func (c *compiler) compileCompositeLit(expr *ast.CompositeLit) (*taivm.Type, error) {
	if expr.Type == nil {
		if err := c.compileMapLit(expr); err != nil {
			return nil, err
		}
		return taivm.FromReflectType(reflect.TypeFor[map[any]any]()), nil
	}

	t, err := c.resolveType(expr.Type)
	if err == nil {
		switch t.Kind {
		case taivm.KindSlice, taivm.KindArray:
			if err := c.compileArrayLit(expr); err != nil {
				return nil, err
			}
			return t, nil
		case taivm.KindMap:
			if err := c.compileMapLit(expr); err != nil {
				return nil, err
			}
			return t, nil
		case taivm.KindStruct:
			if err := c.compileStructLit(expr, expr.Type); err != nil {
				return nil, err
			}
			return t, nil
		}
	}

	// Fallback
	switch tExpr := expr.Type.(type) {
	case *ast.ArrayType:
		if err := c.compileArrayLit(expr); err != nil {
			return nil, err
		}
		return t, nil
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
		if err := c.compileStructLit(expr, tExpr); err != nil {
			return nil, err
		}
		return t, nil
	default:
		if err := c.compileMapLit(expr); err != nil {
			return nil, err
		}
		return t, nil
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
			if _, err := c.compileExpr(elt); err != nil {
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
				if _, err := c.compileExpr(kv.Key); err != nil {
					return err
				}
				if _, err := c.compileExpr(kv.Value); err != nil {
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
				if _, err := c.compileExpr(valExpr); err != nil {
					return err
				}
				c.emit(taivm.OpSetIndex)
			} else {
				// Append at current end
				if _, err := c.compileExpr(valExpr); err != nil {
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
	typeName, _ := c.extractTypeName(t)
	numElts := len(expr.Elts)
	if numElts > 0 {
		if _, ok := expr.Elts[0].(*ast.KeyValueExpr); ok {
			for _, elt := range expr.Elts {
				if err := c.compileStructElement(elt); err != nil {
					return err
				}
			}
		} else {
			fields, ok := c.structFields[typeName]
			if !ok {
				return fmt.Errorf("struct fields unknown for unkeyed literal of %s", typeName)
			}
			if len(fields) < numElts {
				return fmt.Errorf("too many values in unkeyed struct literal")
			}
			for i, elt := range expr.Elts {
				c.loadConst(fields[i])
				if _, err := c.compileExpr(elt); err != nil {
					return err
				}
			}
		}
	}
	c.loadConst(typeName)
	c.loadConst(&taivm.List{Immutable: true})
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
	} else if _, err := c.compileExpr(kv.Key); err != nil {
		return err
	}
	if _, err := c.compileExpr(kv.Value); err != nil {
		return err
	}
	return nil
}

func (c *compiler) compileMapLit(expr *ast.CompositeLit) error {
	for _, elt := range expr.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return fmt.Errorf("element must be key:value")
		}
		if ident, ok := kv.Key.(*ast.Ident); ok {
			c.loadConst(ident.Name)
		} else if _, err := c.compileExpr(kv.Key); err != nil {
			return err
		}
		if _, err := c.compileExpr(kv.Value); err != nil {
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
	if _, err := c.compileExpr(stmt.X); err != nil {
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
		if _, err := c.compileExpr(stmt.Results[0]); err != nil {
			return err
		}
		c.emit(taivm.OpReturn)

	} else {
		for _, r := range stmt.Results {
			if _, err := c.compileExpr(r); err != nil {
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
		if _, err := c.compileExpr(ta.X); err != nil {
			return err
		}
		if _, err := c.compileExpr(ta.Type); err != nil {
			return err
		}
		c.emit(taivm.OpTypeAssertOk)
		c.emit(taivm.OpSwap)
	} else if ie, ok := rhs.(*ast.IndexExpr); ok && len(stmt.Lhs) == 2 {
		if _, err := c.compileExpr(ie.X); err != nil {
			return err
		}
		if _, err := c.compileExpr(ie.Index); err != nil {
			return err
		}
		c.emit(taivm.OpGetIndexOk)
		c.emit(taivm.OpSwap)
	} else {
		if _, err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpUnpack.With(len(stmt.Lhs)))
	}
	for i := 0; i < len(stmt.Lhs); i++ {
		if err := c.compileAssignFromStack(stmt.Lhs[i], stmt.Tok, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileMultiAssignFromMultiRHS(stmt *ast.AssignStmt) error {
	rhsTypes := make([]*taivm.Type, len(stmt.Rhs))
	for i, r := range stmt.Rhs {
		t, err := c.compileExpr(r)
		if err != nil {
			return err
		}
		rhsTypes[i] = t
	}
	for i := len(stmt.Lhs) - 1; i >= 0; i-- {
		if err := c.compileAssignFromStack(stmt.Lhs[i], stmt.Tok, rhsTypes[i]); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileSingleAssign(lhs, rhs ast.Expr, tok token.Token) error {
	t, err := c.compileExpr(rhs)
	if err != nil {
		return err
	}
	return c.compileAssignFromStack(lhs, tok, t)
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
	if err := c.compileAddrOf(lhs.X); err == nil {
		if _, err := c.compileExpr(lhs.Index); err != nil {
			return err
		}
		c.emit(taivm.OpDup2)
		c.emit(taivm.OpGetIndex)
		if _, err := c.compileExpr(rhs); err != nil {
			return err
		}
		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}
		c.emit(taivm.OpSetIndex)
		return nil
	}
	if _, err := c.compileExpr(lhs.X); err != nil {
		return err
	}
	if _, err := c.compileExpr(lhs.Index); err != nil {
		return err
	}
	c.emit(taivm.OpDup2)
	c.emit(taivm.OpGetIndex)
	if _, err := c.compileExpr(rhs); err != nil {
		return err
	}
	if err := c.emitBinaryOp(tok); err != nil {
		return err
	}
	c.emit(taivm.OpSetIndex)
	return nil
}

func (c *compiler) compileSelectorCompoundAssign(lhs *ast.SelectorExpr, rhs ast.Expr, tok token.Token) error {
	if err := c.compileAddrOf(lhs.X); err == nil {
		c.emit(taivm.OpDup)
		c.loadConst(lhs.Sel.Name)
		c.emit(taivm.OpGetAttr)
		if _, err := c.compileExpr(rhs); err != nil {
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
	if _, err := c.compileExpr(lhs.X); err != nil {
		return err
	}
	c.emit(taivm.OpDup)
	c.loadConst(lhs.Sel.Name)
	c.emit(taivm.OpGetAttr)
	if _, err := c.compileExpr(rhs); err != nil {
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
	if v, ok := c.locals[name]; ok {
		c.emit(taivm.OpGetLocal.With(v.index))
		if _, err := c.compileExpr(rhs); err != nil {
			return err
		}
		if err := c.emitBinaryOp(tok); err != nil {
			return err
		}
		c.emit(taivm.OpSetLocal.With(v.index))
		return nil
	}
	idx := c.addConst(name)
	c.emit(taivm.OpLoadVar.With(idx))
	if _, err := c.compileExpr(rhs); err != nil {
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

	if _, err := c.compileExpr(stmt.Cond); err != nil {
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
		if _, err := c.compileExpr(stmt.Tag); err != nil {
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
			if _, err := c.compileExpr(expr); err != nil {
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
	if _, err := c.compileExpr(xExpr); err != nil {
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
				if _, err := c.compileExpr(typeExpr); err != nil {
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
		if _, err := c.compileExpr(stmt.Cond); err != nil {
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
	if _, err := c.compileExpr(stmt.X); err != nil {
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
		if err := c.compileAssignFromStack(stmt.Value, stmt.Tok, nil); err != nil {
			return err
		}
	}
	if stmt.Key != nil {
		if err := c.compileAssignFromStack(stmt.Key, stmt.Tok, nil); err != nil {
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
	// We need to evaluate the function and its arguments at the point of defer.
	// We do this by evaluating them into temporary variables in the current scope,
	// and then creating a closure that calls the function using these temporaries.
	// Since temporaries are assigned once and never modified, capturing them
	// effectively captures their values at this point.

	// 1. Evaluate function expression
	fnTmp := c.nextTmp()
	if _, err := c.compileExpr(stmt.Call.Fun); err != nil {
		return err
	}
	c.emit(taivm.OpDefVar.With(c.addConst(fnTmp)))

	// 2. Evaluate arguments
	var argTmps []string
	for _, arg := range stmt.Call.Args {
		argTmp := c.nextTmp()
		if _, err := c.compileExpr(arg); err != nil {
			return err
		}
		c.emit(taivm.OpDefVar.With(c.addConst(argTmp)))
		argTmps = append(argTmps, argTmp)
	}

	// 3. Construct synthetic CallExpr using the temporaries
	syntheticArgs := make([]ast.Expr, len(argTmps))
	for i, tmp := range argTmps {
		syntheticArgs[i] = &ast.Ident{Name: tmp}
	}
	syntheticCall := &ast.CallExpr{
		Fun:      &ast.Ident{Name: fnTmp},
		Args:     syntheticArgs,
		Ellipsis: stmt.Call.Ellipsis,
	}

	// 4. Compile the closure body using the synthetic call
	sub := &compiler{
		name:         "defer",
		isFunc:       true,
		consts:       make(map[any]int),
		locals:       make(map[string]variable),
		labels:       make(map[string]int),
		unresolved:   make(map[string][]int),
		generics:     c.generics,
		typeParams:   c.typeParams,
		structFields: c.structFields,
		types:        c.types,
		globals:      c.globals,
	}
	if err := sub.compileExprStmt(&ast.ExprStmt{X: syntheticCall}); err != nil {
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

	var explicitType *taivm.Type
	if s.Type != nil {
		var err error
		explicitType, err = c.resolveType(s.Type)
		if err != nil {
			return err
		}
	}

	if len(values) == 1 && len(s.Names) > 1 {
		return c.compileValueSpecSingleRHS(s, values[0], explicitType)
	}
	if len(values) > 0 && len(values) != len(s.Names) {
		return fmt.Errorf("assignment count mismatch: %d = %d", len(s.Names), len(values))
	}
	for i, name := range s.Names {
		valExpr := any(nil)
		if i < len(values) {
			valExpr = values[i]
		}
		if err := c.compileNamedValueDef(name.Name, valExpr, explicitType); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileNamedValueDef(name string, valExpr any, explicitType *taivm.Type) error {
	if name == "_" {
		if e, ok := valExpr.(ast.Expr); ok {
			if _, err := c.compileExpr(e); err != nil {
				return err
			}
		} else {
			c.loadConst(nil)
		}
		c.emit(taivm.OpPop)
		return nil
	}
	var t *taivm.Type
	if explicitType != nil {
		t = explicitType
	}
	if e, ok := valExpr.(ast.Expr); ok {
		rhsType, err := c.compileExpr(e)
		if err != nil {
			return err
		}
		if t == nil {
			t = rhsType
		}
	} else {
		if explicitType != nil {
			c.loadConst(explicitType.Zero())
		} else {
			c.loadConst(nil)
		}
	}
	arg := c.addConst(name)
	if t != nil {
		c.loadConst(t)
		c.emit(taivm.OpDefVar.With(arg | (1 << 23))) // Set typed flag
	} else {
		c.emit(taivm.OpDefVar.With(arg))
	}
	c.globals[name] = t
	return nil
}

func (c *compiler) compileValueSpecSingleRHS(s *ast.ValueSpec, rhs ast.Expr, explicitType *taivm.Type) error {
	if ta, ok := rhs.(*ast.TypeAssertExpr); ok && len(s.Names) == 2 {
		if _, err := c.compileExpr(ta.X); err != nil {
			return err
		}
		if _, err := c.compileExpr(ta.Type); err != nil {
			return err
		}
		c.emit(taivm.OpTypeAssertOk)
		c.emit(taivm.OpSwap)
	} else if ie, ok := rhs.(*ast.IndexExpr); ok && len(s.Names) == 2 {
		if _, err := c.compileExpr(ie.X); err != nil {
			return err
		}
		if _, err := c.compileExpr(ie.Index); err != nil {
			return err
		}
		c.emit(taivm.OpGetIndexOk)
		c.emit(taivm.OpSwap)
	} else {
		if _, err := c.compileExpr(rhs); err != nil {
			return err
		}
		c.emit(taivm.OpUnpack.With(len(s.Names)))
	}
	for _, name := range s.Names {
		if name.Name == "_" {
			c.emit(taivm.OpPop)
			continue
		}
		if explicitType != nil {
			c.loadConst(explicitType)
			c.emit(taivm.OpDefVar.With(c.addConst(name.Name) | (1 << 23)))
			c.globals[name.Name] = explicitType
		} else {
			c.emit(taivm.OpDefVar.With(c.addConst(name.Name)))
		}
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

	if t.Kind == taivm.KindStruct {
		var fields []string
		for _, f := range t.Fields {
			fields = append(fields, f.Name)
		}
		c.structFields[spec.Name.Name] = fields
	} else if t.Kind == taivm.KindExternal {
		rt := t.External
		if rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() == reflect.Struct {
			var fields []string
			for i := 0; i < rt.NumField(); i++ {
				fields = append(fields, rt.Field(i).Name)
			}
			c.structFields[spec.Name.Name] = fields
		}
	}

	// Handle type alias: type T = S
	if spec.Assign != token.NoPos {
		c.types[spec.Name.Name] = t
		c.loadConst(t)
		c.emit(taivm.OpDefVar.With(c.addConst(spec.Name.Name)))
		return nil
	}
	// Defined type: type T S. Copy the type to avoid mutating shared types.
	newT := *t
	newT.Name = spec.Name.Name
	t = &newT
	c.types[spec.Name.Name] = t
	c.loadConst(t)
	c.emit(taivm.OpDefVar.With(c.addConst(spec.Name.Name)))
	return nil
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
		typeName, _ := c.extractTypeName(e)
		if t, ok := c.types[typeName]; ok {
			return t, nil
		}
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
	if rt, ok := basicTypes[e.Name]; ok {
		return taivm.FromReflectType(rt), nil
	}
	return taivm.FromReflectType(reflect.TypeFor[any]()), nil
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

func (c *compiler) compileAssignFromStack(lhs ast.Expr, tok token.Token, rhsType *taivm.Type) error {
	switch e := lhs.(type) {
	case *ast.Ident:
		return c.compileIdentAssign(e, tok, rhsType)
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

func (c *compiler) compileIdentAssign(e *ast.Ident, tok token.Token, rhsType *taivm.Type) error {
	name := e.Name
	if name == "_" {
		c.emit(taivm.OpPop)
		return nil
	}
	if v, ok := c.locals[name]; ok {
		c.emit(taivm.OpSetLocal.With(v.index))
		return nil
	}
	idx := c.addConst(name)
	if tok == token.DEFINE {
		if rhsType != nil {
			c.loadConst(rhsType)
			c.emit(taivm.OpDefVar.With(idx | (1 << 23)))
		} else {
			c.emit(taivm.OpDefVar.With(idx))
		}
		c.globals[name] = rhsType
	} else {
		c.emit(taivm.OpSetVar.With(idx))
	}
	return nil
}

func (c *compiler) compileIndexAssign(e *ast.IndexExpr) error {
	tmpIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(tmpIdx))
	if err := c.compileAddrOf(e.X); err == nil {
		if _, err := c.compileExpr(e.Index); err != nil {
			return err
		}
		c.emit(taivm.OpLoadVar.With(tmpIdx))
		c.emit(taivm.OpSetIndex)
		return nil
	}
	if _, err := c.compileExpr(e.X); err != nil {
		return err
	}
	if _, err := c.compileExpr(e.Index); err != nil {
		return err
	}
	c.emit(taivm.OpLoadVar.With(tmpIdx))
	c.emit(taivm.OpSetIndex)
	return nil
}

func (c *compiler) compileSelectorAssign(e *ast.SelectorExpr) error {
	tmpIdx := c.addConst(c.nextTmp())
	c.emit(taivm.OpDefVar.With(tmpIdx))
	if err := c.compileAddrOf(e.X); err == nil {
		c.loadConst(e.Sel.Name)
		c.emit(taivm.OpLoadVar.With(tmpIdx))
		c.emit(taivm.OpSetAttr)
		return nil
	}
	if _, err := c.compileExpr(e.X); err != nil {
		return err
	}
	c.loadConst(e.Sel.Name)
	c.emit(taivm.OpLoadVar.With(tmpIdx))
	c.emit(taivm.OpSetAttr)
	return nil
}

func (c *compiler) compileAddrOf(expr ast.Expr) error {
	switch x := expr.(type) {
	case *ast.Ident:
		idx := c.addConst(x.Name)
		c.emit(taivm.OpAddrOf.With(idx))
		return nil
	case *ast.IndexExpr:
		if _, err := c.compileExpr(x.X); err != nil {
			return err
		}
		if _, err := c.compileExpr(x.Index); err != nil {
			return err
		}
		c.emit(taivm.OpAddrOfIndex)
		return nil
	case *ast.SelectorExpr:
		if _, err := c.compileExpr(x.X); err != nil {
			return err
		}
		c.loadConst(x.Sel.Name)
		c.emit(taivm.OpAddrOfAttr)
		return nil
	case *ast.StarExpr:
		_, err := c.compileExpr(x.X)
		return err
	case *ast.ParenExpr:
		return c.compileAddrOf(x.X)
	}
	return fmt.Errorf("cannot take address of %T", expr)
}

func (c *compiler) compileDerefAssign(e *ast.StarExpr) error {
	if _, err := c.compileExpr(e.X); err != nil {
		return err
	}
	c.emit(taivm.OpSwap)
	c.emit(taivm.OpSetDeref)
	return nil
}
