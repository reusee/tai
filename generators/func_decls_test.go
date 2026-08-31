package generators

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func TestFuncDeclsHandleConfigDedupes(t *testing.T) {
	ctx := cuecontext.New()
	value := ctx.CompileString(
		`[{name: "alpha"}, {name: "alpha", description: "duplicate"}, {name: "beta"}]`)
	f := FuncDecls{}
	def, err := f.HandleConfig("test", []*cue.Value{&value})
	if err != nil {
		t.Fatal(err)
	}
	decls, ok := def.(*FuncDecls)
	if !ok {
		t.Fatalf("unexpected def type %T", def)
	}
	if len(*decls) != 2 {
		t.Fatalf("expected 2 decls after dedupe, got %d", len(*decls))
	}
	if (*decls)[0].Name != "alpha" || (*decls)[0].Description != "" {
		t.Fatalf("expected first occurrence to win, got %+v", (*decls)[0])
	}
	if (*decls)[1].Name != "beta" {
		t.Fatalf("unexpected decl: %+v", (*decls)[1])
	}
}
