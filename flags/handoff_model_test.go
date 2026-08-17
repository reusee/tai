package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/reusee/dscope"
)

func TestHandoffModelFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-handoff-model", "gemini-flash", "-handoff-model", "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	result.Call(func(models HandoffModels) {
		if len(models) != 2 || models[0] != "gemini-flash" || models[1] != "deepseek-chat" {
			t.Fatalf("expected [gemini-flash deepseek-chat], got %v", models)
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
	m := HandoffModels(nil)
	keys := m.Keys()
	if _, ok := keys["-handoff-model"]; !ok {
		t.Fatal("-handoff-model flag not registered in Keys()")
	}
}

func TestHandoffModelConfig(t *testing.T) {
	ctx := cuecontext.New()

	t.Run("String", func(t *testing.T) {
		v := ctx.CompileString(`"gemini-flash"`)
		m := HandoffModels(nil)
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*HandoffModels)
		if !ok {
			t.Fatalf("expected *HandoffModels, got %T", def)
		}
		if len(*ret) != 1 || (*ret)[0] != "gemini-flash" {
			t.Fatalf("expected [gemini-flash], got %v", *ret)
		}
	})

	t.Run("List", func(t *testing.T) {
		v := ctx.CompileString(`["gemini-flash", "deepseek-chat"]`)
		m := HandoffModels(nil)
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*HandoffModels)
		if !ok {
			t.Fatalf("expected *HandoffModels, got %T", def)
		}
		if len(*ret) != 2 || (*ret)[0] != "gemini-flash" || (*ret)[1] != "deepseek-chat" {
			t.Fatalf("expected [gemini-flash deepseek-chat], got %v", *ret)
		}
	})

	t.Run("Accumulates", func(t *testing.T) {
		v1 := ctx.CompileString(`"first"`)
		v2 := ctx.CompileString(`["second", "third"]`)
		m := HandoffModels(nil)
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v1, &v2})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*HandoffModels)
		if !ok {
			t.Fatalf("expected *HandoffModels, got %T", def)
		}
		if len(*ret) != 3 || (*ret)[0] != "first" || (*ret)[1] != "second" || (*ret)[2] != "third" {
			t.Fatalf("expected [first second third], got %v", *ret)
		}
	})
}
