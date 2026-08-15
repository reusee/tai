package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/reusee/dscope"
)

func TestHandoffModelFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-handoff-model", "gemini-flash"})
	if err != nil {
		t.Fatal(err)
	}
	result.Call(func(name HandoffModel) {
		if name != "gemini-flash" {
			t.Fatalf("expected gemini-flash, got %q", name)
		}
	})
}

func TestHandoffModelFlagNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-handoff-model"})
	if err == nil {
		t.Fatal("expected error for handoff-model with no argument")
	}
}

func TestHandoffModelKeys(t *testing.T) {
	m := HandoffModel("")
	keys := m.Keys()
	if _, ok := keys["-handoff-model"]; !ok {
		t.Fatal("-handoff-model flag not registered in Keys()")
	}
}

func TestHandoffModelConfig(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`"gemini-flash"`)

	m := HandoffModel("")
	def, err := m.HandleConfig("handoff_model", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := def.(*HandoffModel)
	if !ok {
		t.Fatalf("expected *HandoffModel, got %T", def)
	}
	if *ret != "gemini-flash" {
		t.Fatalf("expected gemini-flash, got %q", *ret)
	}

	paths := m.ConfigPaths()
	if len(paths) != 1 || paths[0] != "handoff_model" {
		t.Fatalf("unexpected config paths: %v", paths)
	}
}
