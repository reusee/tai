package taipy

import (
	"strings"
	"testing"

	"github.com/reusee/tai/taivm"
	"go.starlark.net/syntax"
)

// Mock types to force errors in sub-expressions
type failExpr struct {
	syntax.Literal
}

type failStmt struct {
	syntax.ExprStmt
}

func TestCompilerCoverage(t *testing.T) {
	c := newCompiler("test")
	lit := &syntax.Literal{Token: syntax.INT, Value: int64(1)}
	fExpr := &failExpr{}
	fStmt := &failStmt{}

	expectError := func(name string, err error, sub string) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: expected error, got nil", name)
		} else if sub != "" && !strings.Contains(err.Error(), sub) {
			t.Errorf("%s: error %q does not contain %q", name, err, sub)
		}
	}

	// 1. extractParamNames errors
	// These are usually reached via compileDef or compileLambdaExpr, but unit testing helper is direct.
	// "variadic parameter must be last"
	_, _, _, err := c.extractParamNames([]syntax.Expr{
		&syntax.UnaryExpr{Op: syntax.STAR, X: &syntax.Ident{Name: "args"}},
		&syntax.Ident{Name: "b"},
	})
	expectError("extractParamNames variadic not last", err, "variadic parameter must be last")

	// "non-default argument follows default argument"
	_, _, _, err = c.extractParamNames([]syntax.Expr{
		&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: lit},
		&syntax.Ident{Name: "b"},
	})
	expectError("extractParamNames non-default after default", err, "non-default argument follows default argument")

	// "variadic parameter must be identifier"
	_, _, _, err = c.extractParamNames([]syntax.Expr{
		&syntax.UnaryExpr{Op: syntax.STAR, X: lit},
	})
	expectError("extractParamNames variadic bad type", err, "variadic parameter must be identifier")

	// "parameter name must be identifier" (default value case)
	_, _, _, err = c.extractParamNames([]syntax.Expr{
		&syntax.BinaryExpr{Op: syntax.EQ, X: lit, Y: lit},
	})
	expectError("extractParamNames param name bad type", err, "parameter name must be identifier")

	// "complex parameters not supported"
	_, _, _, err = c.extractParamNames([]syntax.Expr{lit})
	expectError("extractParamNames complex", err, "complex parameters not supported")

	// 2. compileStore errors
	// "unsupported variable type"
	expectError("compileStore literal", c.compileStore(lit), "unsupported variable type")

	// Sub-expression errors in compileStore
	expectError("compileStore List elem", c.compileStore(&syntax.ListExpr{List: []syntax.Expr{fExpr}}), "")
	expectError("compileStore Tuple elem", c.compileStore(&syntax.TupleExpr{List: []syntax.Expr{fExpr}}), "")
	expectError("compileStore Dot X", c.compileStore(&syntax.DotExpr{X: fExpr, Name: &syntax.Ident{Name: "a"}}), "")
	expectError("compileStore Index X", c.compileStore(&syntax.IndexExpr{X: fExpr, Y: lit}), "")
	expectError("compileStore Index Y", c.compileStore(&syntax.IndexExpr{X: lit, Y: fExpr}), "")
	expectError("compileStore Slice X", c.compileStore(&syntax.SliceExpr{X: fExpr}), "")
	expectError("compileStore Slice Lo", c.compileStore(&syntax.SliceExpr{X: lit, Lo: fExpr}), "")

	// 3. compileAssign & compileAugmentedAssign
	// compileAssign just dispatches.
	// compileAugmentedAssign "augmented assignment op ... not supported"
	expectError("compileAugmentedAssign bad op", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.EQ}), "augmented assignment op")
	
	// compileAugmentedAssign "unsupported augmented assignment target"
	expectError("compileAugmentedAssign bad target", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: lit, RHS: lit}), "unsupported augmented assignment target")

	// compileAugmentedAssign sub-expression errors
	expectError("compileAugmentedAssign Ident RHS", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.Ident{Name: "a"}, RHS: fExpr}), "")
	expectError("compileAugmentedAssign Index X", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: fExpr, Y: lit}, RHS: lit}), "")
	expectError("compileAugmentedAssign Index Y", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: lit, Y: fExpr}, RHS: lit}), "")
	expectError("compileAugmentedAssign Index RHS", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.IndexExpr{X: lit, Y: lit}, RHS: fExpr}), "")
	expectError("compileAugmentedAssign Dot X", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.DotExpr{X: fExpr, Name: &syntax.Ident{Name: "a"}}, RHS: lit}), "")
	expectError("compileAugmentedAssign Dot RHS", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.DotExpr{X: lit, Name: &syntax.Ident{Name: "a"}}, RHS: fExpr}), "")
	expectError("compileAugmentedAssign Slice X", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: fExpr}, RHS: lit}), "")
	expectError("compileAugmentedAssign Slice RHS", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: lit}, RHS: fExpr}), "")
	// Slice Args error in AugAssign
	expectError("compileAugmentedAssign Slice Lo", c.compileAugmentedAssign(&syntax.AssignStmt{Op: syntax.PLUS_EQ, LHS: &syntax.SliceExpr{X: lit, Lo: fExpr}, RHS: lit}), "")

	// compileSimpleAssign errors
	expectError("compileSimpleAssign bad target", c.compileSimpleAssign(lit, lit), "unsupported assignment target")
	
	// compileSimpleAssign sub-expression errors
	// IndexExpr
	expectError("compileSimpleAssign Index X", c.compileSimpleAssign(&syntax.IndexExpr{X: fExpr, Y: lit}, lit), "")
	expectError("compileSimpleAssign Index Y", c.compileSimpleAssign(&syntax.IndexExpr{X: lit, Y: fExpr}, lit), "")
	expectError("compileSimpleAssign Index RHS", c.compileSimpleAssign(&syntax.IndexExpr{X: lit, Y: lit}, fExpr), "")
	
	// SliceExpr
	expectError("compileSimpleAssign Slice X", c.compileSimpleAssign(&syntax.SliceExpr{X: fExpr}, lit), "")
	expectError("compileSimpleAssign Slice Args", c.compileSimpleAssign(&syntax.SliceExpr{X: lit, Lo: fExpr}, lit), "")
	expectError("compileSimpleAssign Slice RHS", c.compileSimpleAssign(&syntax.SliceExpr{X: lit}, fExpr), "")
	
	// DotExpr
	expectError("compileSimpleAssign Dot X", c.compileSimpleAssign(&syntax.DotExpr{X: fExpr, Name: &syntax.Ident{Name: "a"}}, lit), "")
	expectError("compileSimpleAssign Dot RHS", c.compileSimpleAssign(&syntax.DotExpr{X: lit, Name: &syntax.Ident{Name: "a"}}, fExpr), "")
	
	// List/Tuple unpacking recursive error in SimpleAssign
	expectError("compileSimpleAssign List", c.compileSimpleAssign(&syntax.ListExpr{List: []syntax.Expr{fExpr}}, lit), "")
	
	// Ident RHS
	expectError("compileSimpleAssign Ident RHS", c.compileSimpleAssign(&syntax.Ident{Name: "a"}, fExpr), "")

	// 4. compileBinaryExpr errors
	// "unsupported binary op"
	expectError("compileBinaryExpr bad op", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.DEF, X: lit, Y: lit}), "unsupported binary op")
	// Sub-expression errors
	expectError("compileBinaryExpr AND X", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.AND, X: fExpr, Y: lit}), "")
	expectError("compileBinaryExpr AND Y", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.AND, X: lit, Y: fExpr}), "")
	expectError("compileBinaryExpr OR X", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.OR, X: fExpr, Y: lit}), "")
	expectError("compileBinaryExpr OR Y", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.OR, X: lit, Y: fExpr}), "")
	expectError("compileBinaryExpr Add X", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.PLUS, X: fExpr, Y: lit}), "")
	expectError("compileBinaryExpr Add Y", c.compileBinaryExpr(&syntax.BinaryExpr{Op: syntax.PLUS, X: lit, Y: fExpr}), "")

	// 5. compileCallExpr errors
	// "positional argument follows keyword argument"
	expectError("compileCallExpr pos after kw", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: lit},
			lit,
		},
	}), "positional argument follows keyword argument")
	
	// Sub-expression errors
	expectError("compileCallExpr Fn", c.compileCallExpr(&syntax.CallExpr{Fn: fExpr}), "")
	expectError("compileCallExpr Arg", c.compileCallExpr(&syntax.CallExpr{Fn: lit, Args: []syntax.Expr{fExpr}}), "")
	
	// Keyword arg value error (dynamic path)
	// We force dynamic path by using a binary expr which is not a simple arg
	expectError("compileCallExpr KwArg Value", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: fExpr},
		},
	}), "")
	
	// Star arg error
	expectError("compileCallExpr Star Arg", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			&syntax.UnaryExpr{Op: syntax.STAR, X: fExpr},
		},
	}), "")
	
	// StarStar arg error
	expectError("compileCallExpr StarStar Arg", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			&syntax.UnaryExpr{Op: syntax.STARSTAR, X: fExpr},
		},
	}), "")

	// Flush errors (deferred errors in loops)
	// flushPos error via keyword encounter
	expectError("compileCallExpr flushPos via kw", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			fExpr, // pending pos
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: lit},
		},
	}), "")
	
	// flushKw error via starstar
	expectError("compileCallExpr flushKw via starstar", c.compileCallExpr(&syntax.CallExpr{
		Fn: lit,
		Args: []syntax.Expr{
			&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: fExpr}, // pending kw
			&syntax.UnaryExpr{Op: syntax.STARSTAR, X: &syntax.Ident{Name: "d"}},
		},
	}), "")

	// 6. compileComprehension errors
	// "dict comprehension body must be DictEntry"
	expectError("compileComprehension Dict Body", c.compileComprehension(&syntax.Comprehension{
		Curly: true,
		Body:  lit, // Not DictEntry
	}), "dict comprehension body must be DictEntry")

	// "unsupported comprehension clause"
	expectError("compileComprehension Clause", c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{&mockClause{}},
	}), "unsupported comprehension clause")

	// Sub-expression errors
	expectError("compileComprehension List Body", c.compileComprehension(&syntax.Comprehension{
		Body:    fExpr,
		Clauses: []syntax.Node{},
	}), "")
	
	expectError("compileComprehension Dict Key", c.compileComprehension(&syntax.Comprehension{
		Curly:   true,
		Body:    &syntax.DictEntry{Key: fExpr, Value: lit},
		Clauses: []syntax.Node{},
	}), "")
	
	expectError("compileComprehension Dict Value", c.compileComprehension(&syntax.Comprehension{
		Curly:   true,
		Body:    &syntax.DictEntry{Key: lit, Value: fExpr},
		Clauses: []syntax.Node{},
	}), "")

	expectError("compileComprehension For X", c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{&syntax.ForClause{Vars: &syntax.Ident{Name: "x"}, X: fExpr}},
	}), "")
	
	expectError("compileComprehension For Vars", c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{&syntax.ForClause{Vars: fExpr, X: lit}}, // fExpr fails compileStore
	}), "")
	
	expectError("compileComprehension If Cond", c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{&syntax.IfClause{Cond: fExpr}},
	}), "")
	
	// Recursive clause error
	expectError("compileComprehension Recursive", c.compileComprehension(&syntax.Comprehension{
		Body:    lit,
		Clauses: []syntax.Node{
			&syntax.IfClause{Cond: lit},
			&syntax.IfClause{Cond: fExpr},
		},
	}), "")

	// 7. compileStmt & compileExpr defaults and other types
	expectError("compileStmt default", c.compileStmt(fStmt), "unsupported statement type")
	expectError("compileExpr default", c.compileExpr(fExpr), "unsupported expression")

	// compileIf errors
	expectError("compileIf Cond", c.compileIf(&syntax.IfStmt{Cond: fExpr}), "")
	expectError("compileIf True", c.compileIf(&syntax.IfStmt{Cond: lit, True: []syntax.Stmt{fStmt}}), "")
	expectError("compileIf False", c.compileIf(&syntax.IfStmt{Cond: lit, True: []syntax.Stmt{}, False: []syntax.Stmt{fStmt}}), "")

	// compileWhile errors
	expectError("compileWhile Cond", c.compileWhile(&syntax.WhileStmt{Cond: fExpr}), "")
	expectError("compileWhile Body", c.compileWhile(&syntax.WhileStmt{Cond: lit, Body: []syntax.Stmt{fStmt}}), "")

	// compileFor errors
	expectError("compileFor X", c.compileFor(&syntax.ForStmt{X: fExpr, Vars: &syntax.Ident{Name: "x"}}), "")
	expectError("compileFor Vars", c.compileFor(&syntax.ForStmt{X: lit, Vars: fExpr}), "")
	expectError("compileFor Body", c.compileFor(&syntax.ForStmt{X: lit, Vars: &syntax.Ident{Name: "x"}, Body: []syntax.Stmt{fStmt}}), "")

	// compileDef errors
	expectError("compileDef Body", c.compileDef(&syntax.DefStmt{Name: &syntax.Ident{Name: "f"}, Body: []syntax.Stmt{fStmt}}), "")
	// Default value error
	expectError("compileDef Default", c.compileDef(&syntax.DefStmt{
		Name: &syntax.Ident{Name: "f"},
		Params: []syntax.Expr{&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: fExpr}},
	}), "")

	// compileLambdaExpr errors
	expectError("compileLambdaExpr Body", c.compileLambdaExpr(&syntax.LambdaExpr{Body: fExpr}), "")
	// compileLambdaExpr Default
	expectError("compileLambdaExpr Default", c.compileLambdaExpr(&syntax.LambdaExpr{
		Params: []syntax.Expr{&syntax.BinaryExpr{Op: syntax.EQ, X: &syntax.Ident{Name: "a"}, Y: fExpr}},
		Body: lit,
	}), "")

	// Reset compiler to ensure clean state (no loops)
	c = newCompiler("test_branch")
	// compileBranch outside loop
	expectError("compileBranch outside loop", c.compileBranch(&syntax.BranchStmt{Token: syntax.BREAK}), "outside loop")

	// compileLoad doesn't really have error paths other than emit, but check types?
	// It relies on syntax pkg to give valid structure.

	// Other expressions
	expectError("compileListExpr", c.compileListExpr(&syntax.ListExpr{List: []syntax.Expr{fExpr}}), "")
	expectError("compileDictExpr", c.compileDictExpr(&syntax.DictExpr{List: []syntax.Expr{&syntax.DictEntry{Key: fExpr, Value: lit}}}), "")
	expectError("compileIndexExpr", c.compileIndexExpr(&syntax.IndexExpr{X: fExpr, Y: lit}), "")
	expectError("compileTupleExpr", c.compileTupleExpr(&syntax.TupleExpr{List: []syntax.Expr{fExpr}}), "")
	expectError("compileSliceExpr", c.compileSliceExpr(&syntax.SliceExpr{X: fExpr}), "")
	expectError("compileDotExpr", c.compileDotExpr(&syntax.DotExpr{X: fExpr, Name: &syntax.Ident{Name: "a"}}), "")
	expectError("compileCondExpr", c.compileCondExpr(&syntax.CondExpr{Cond: fExpr}), "")
	expectError("compileUnaryExpr", c.compileUnaryExpr(&syntax.UnaryExpr{Op: syntax.PLUS, X: fExpr}), "")
}

type mockClause struct {
	syntax.ForClause
}

func TestMiscCoverage(t *testing.T) {
	// isComparable coverage
	if isComparable([]int{1}) {
		t.Error("slice should not be comparable")
	}
	if !isComparable(1) {
		t.Error("int should be comparable")
	}

	// Native func helpers
	if !isFloat(1.0) {
		t.Error("1.0 should be float")
	}
	if isFloat(1) {
		t.Error("1 should not be float")
	}
}

func TestManualASTCoverage(t *testing.T) {
	c := newCompiler("manual")
	// Compile: 2 ** 3
	// We want to verify it emits OpPow.
	
	err := c.compileBinaryExpr(&syntax.BinaryExpr{
		Op: syntax.STARSTAR,
		X:  &syntax.Literal{Value: int64(2)},
		Y:  &syntax.Literal{Value: int64(3)},
	})
	if err != nil {
		t.Errorf("compileBinaryExpr STARSTAR failed: %v", err)
	}
	
	// Check last opcode
	// OpPow is emitted last.
	// Consts are loaded before that.
	if len(c.code) == 0 {
		t.Fatal("no code emitted")
	}
	lastOp := c.code[len(c.code)-1]
	if lastOp != taivm.OpPow {
		t.Errorf("expected OpPow, got %v", lastOp)
	}
}

func TestStoreCoverage(t *testing.T) {
	// Verify compileStore path for SliceExpr (reached via unpacking)
	src := `
l = [1, 2]
[l[0:1]] = [[3]]
`
	vm, err := NewVM("test", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, err := range vm.Run {
		if err != nil {
			t.Fatal(err)
		}
	}
	
	if val, ok := vm.Get("l"); !ok {
		t.Error("l not found")
	} else if l, ok := val.(*taivm.List); !ok || len(l.Elements) != 2 || l.Elements[0] != int64(3) {
		t.Errorf("l = %v", val)
	}
}

func TestLambdaErrors(t *testing.T) {
	c := newCompiler("test")
	lit := &syntax.Literal{Token: syntax.INT, Value: int64(1)}

	// compileLambdaExpr extractParamNames error
	// triggers if err != nil check
	expectError := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}

	expectError("compileLambdaExpr Params", c.compileLambdaExpr(&syntax.LambdaExpr{
		Params: []syntax.Expr{lit},
		Body: lit,
	}))
}
