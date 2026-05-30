package generators

import (
	"fmt"
	"strings"
	"testing"
)

var FuncNow = &Function{
	Decl: FuncDecl{
		Name:        "now",
		Description: "get current time",
		Params: Vars{
			{
				Name:        "timezone",
				Type:        TypeString,
				Description: "timezone",
			},
		},
	},
}

func TestMakeFunc(t *testing.T) {
	// simple function
	add := func(a, b int) int { return a + b }
	f, err := MakeFunc("add", add)
	if err != nil {
		t.Fatal(err)
	}
	if f.Decl.Name != "add" {
		t.Fatalf("got %v", f.Decl.Name)
	}
	if len(f.Decl.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(f.Decl.Params))
	}
	if f.Decl.Params[0].Name != "arg0" || f.Decl.Params[0].Type != TypeInteger {
		t.Errorf("param0: %+v", f.Decl.Params[0])
	}
	// call
	res, err := f.Func(map[string]any{"arg0": 1, "arg1": 2})
	if err != nil {
		t.Fatal(err)
	}
	if res["result0"] != 3 {
		t.Errorf("unexpected result: %v", res)
	}

	// function with error return
	div := func(a, b int) (int, error) {
		if b == 0 {
			return 0, fmt.Errorf("division by zero")
		}
		return a / b, nil
	}
	f, err = MakeFunc("div", div)
	if err != nil {
		t.Fatal(err)
	}
	// call success
	res, err = f.Func(map[string]any{"arg0": 6, "arg1": 2})
	if err != nil {
		t.Fatal(err)
	}
	if res["result0"] != 3 {
		t.Errorf("unexpected result: %v", res)
	}
	// call error
	_, err = f.Func(map[string]any{"arg0": 1, "arg1": 0})
	if err == nil || err.Error() != "division by zero" {
		t.Errorf("expected division error, got %v", err)
	}

	// function with struct
	type point struct{ X, Y int }
	dist := func(p point) int { return p.X*p.X + p.Y*p.Y }
	f, err = MakeFunc("dist", dist)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Decl.Params) != 1 {
		t.Fatal("expected 1 param")
	}
	param := f.Decl.Params[0]
	if param.Type != TypeObject {
		t.Errorf("expected TypeObject, got %v", param.Type)
	}
	if len(param.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(param.Properties))
	}
	res, err = f.Func(map[string]any{"arg0": map[string]any{"X": 3, "Y": 4}})
	if err != nil {
		t.Fatal(err)
	}
	if res["result0"] != 25 {
		t.Errorf("unexpected result: %v", res)
	}

	// function with pointer
	inc := func(x *int) *int {
		if x == nil {
			y := 1
			return &y
		}
		y := *x + 1
		return &y
	}
	f, err = MakeFunc("inc", inc)
	if err != nil {
		t.Fatal(err)
	}
	res, err = f.Func(map[string]any{"arg0": 5})
	if err != nil {
		t.Fatal(err)
	}
	if ptr, ok := res["result0"].(*int); !ok || *ptr != 6 {
		t.Errorf("unexpected result: %v", res)
	}

	// missing arg
	_, err = f.Func(map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "missing argument") {
		t.Errorf("expected missing argument error, got %v", err)
	}
}

