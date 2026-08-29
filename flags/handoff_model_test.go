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
	result.Call(func(model HandoffModel) {
		if string(model) != "gemini-flash" {
			t.Fatalf("expected gemini-flash, got %v", model)
		}
	})
}

func TestHandoffModelFlagOverwritesPrevious(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-handoff-model", "gemini-flash", "-handoff-model", "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	result.Call(func(model HandoffModel) {
		if string(model) != "deepseek-chat" {
			t.Fatalf("expected deepseek-chat (last flag wins), got %v", model)
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

	t.Run("String", func(t *testing.T) {
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
		if string(*ret) != "gemini-flash" {
			t.Fatalf("expected gemini-flash, got %v", *ret)
		}
	})

	t.Run("FirstValueWins", func(t *testing.T) {
		v1 := ctx.CompileString(`"project-model"`)
		v2 := ctx.CompileString(`"global-model"`)
		m := HandoffModel("")
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v1, &v2})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*HandoffModel)
		if !ok {
			t.Fatalf("expected *HandoffModel, got %T", def)
		}
		if string(*ret) != "project-model" {
			t.Fatalf("expected project-model (first value wins), got %v", *ret)
		}
	})

	t.Run("EmptyFirstFallsThroughToLaterRoot", func(t *testing.T) {
		v1 := ctx.CompileString(`""`)
		v2 := ctx.CompileString(`"global-model"`)
		m := HandoffModel("")
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v1, &v2})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*HandoffModel)
		if !ok {
			t.Fatalf("expected *HandoffModel, got %T", def)
		}
		if string(*ret) != "global-model" {
			t.Fatalf("expected global-model (empty first value falls through), got %v", *ret)
		}
	})

	t.Run("ListReturnsError", func(t *testing.T) {
		v := ctx.CompileString(`["gemini-flash", "deepseek-chat"]`)
		m := HandoffModel("")
		_, err := m.HandleConfig("handoff_model", []*cue.Value{&v})
		if err == nil {
			t.Fatal("expected error for list value")
		}
	})

	t.Run("EmptyStringReturnsNil", func(t *testing.T) {
		v := ctx.CompileString(`""`)
		m := HandoffModel("")
		def, err := m.HandleConfig("handoff_model", []*cue.Value{&v})
		if err != nil {
			t.Fatal(err)
		}
		if def != nil {
			t.Fatalf("expected nil for empty string, got %v", def)
		}
	})
}
