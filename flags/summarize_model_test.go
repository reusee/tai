package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/reusee/dscope"
)

func TestSummarizeModelFlag(t *testing.T) {
	scope := dscope.New(Module{})
	result, err := Parse(scope, []string{"-summarize-model", "gemini-flash"})
	if err != nil {
		t.Fatal(err)
	}
	result.Call(func(name SummarizeModel) {
		if name != "gemini-flash" {
			t.Fatalf("expected gemini-flash, got %q", name)
		}
	})
}

func TestSummarizeModelFlagNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-summarize-model"})
	if err == nil {
		t.Fatal("expected error for summarize-model with no argument")
	}
}

func TestSummarizeModelKeys(t *testing.T) {
	m := SummarizeModel("")
	keys := m.Keys()
	if _, ok := keys["-summarize-model"]; !ok {
		t.Fatal("-summarize-model flag not registered in Keys()")
	}
}

func TestSummarizeModelConfig(t *testing.T) {
	ctx := cuecontext.New()
	v := ctx.CompileString(`"gemini-flash"`)

	m := SummarizeModel("")
	def, err := m.HandleConfig("summarize_model", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := def.(*SummarizeModel)
	if !ok {
		t.Fatalf("expected *SummarizeModel, got %T", def)
	}
	if *ret != "gemini-flash" {
		t.Fatalf("expected gemini-flash, got %q", *ret)
	}

	paths := m.ConfigPaths()
	if len(paths) != 1 || paths[0] != "summarize_model" {
		t.Fatalf("unexpected config paths: %v", paths)
	}
}
