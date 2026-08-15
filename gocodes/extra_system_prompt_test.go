package gocodes

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func TestExtraSystemPromptConfigPaths(t *testing.T) {
	e := ExtraSystemPrompt(nil)
	paths := e.ConfigPaths()
	if len(paths) != 1 || paths[0] != "go.extra_system_prompt" {
		t.Fatalf("expected [go.extra_system_prompt], got %v", paths)
	}
}

func TestExtraSystemPromptHandleConfigAggregatesStrings(t *testing.T) {
	ctx := cuecontext.New()

	v1 := ctx.CompileString(`"prompt1"`)
	v2 := ctx.CompileString(`"prompt2"`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("go.extra_system_prompt", []*cue.Value{&v1, &v2})
	if err != nil {
		t.Fatal(err)
	}

	ret, ok := result.(*ExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *ExtraSystemPrompt, got %T", result)
	}

	if len(*ret) != 2 {
		t.Fatalf("expected 2 prompts, got %d: %v", len(*ret), *ret)
	}
	if (*ret)[0] != "prompt1" || (*ret)[1] != "prompt2" {
		t.Fatalf("expected [prompt1, prompt2], got %v", *ret)
	}
}

func TestExtraSystemPromptHandleConfigAggregatesList(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`["prompt1", "prompt2", "prompt3"]`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("go.extra_system_prompt", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}

	ret, ok := result.(*ExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *ExtraSystemPrompt, got %T", result)
	}

	if len(*ret) != 3 {
		t.Fatalf("expected 3 prompts, got %d: %v", len(*ret), *ret)
	}
	if (*ret)[0] != "prompt1" || (*ret)[1] != "prompt2" || (*ret)[2] != "prompt3" {
		t.Fatalf("expected [prompt1, prompt2, prompt3], got %v", *ret)
	}
}

func TestExtraSystemPromptHandleConfigMixedStringAndList(t *testing.T) {
	ctx := cuecontext.New()

	v1 := ctx.CompileString(`"single"`)
	v2 := ctx.CompileString(`["list1", "list2"]`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("go.extra_system_prompt", []*cue.Value{&v1, &v2})
	if err != nil {
		t.Fatal(err)
	}

	ret, ok := result.(*ExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *ExtraSystemPrompt, got %T", result)
	}

	if len(*ret) != 3 {
		t.Fatalf("expected 3 prompts, got %d: %v", len(*ret), *ret)
	}
	if (*ret)[0] != "single" || (*ret)[1] != "list1" || (*ret)[2] != "list2" {
		t.Fatalf("expected [single, list1, list2], got %v", *ret)
	}
}

func TestExtraSystemPromptHandleConfigSkipsEmptyString(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`""`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("go.extra_system_prompt", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}

	ret, ok := result.(*ExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *ExtraSystemPrompt, got %T", result)
	}

	if len(*ret) != 0 {
		t.Fatalf("expected 0 prompts for empty string, got %d: %v", len(*ret), *ret)
	}
}

func TestFamilyExtraSystemPromptHandleConfig(t *testing.T) {
	ctx := cuecontext.New()

	v := ctx.CompileString(`{
		gemini: "gemini prompt"
		deepseek: ["deepseek one", "deepseek two"]
	}`)

	f := FamilyExtraSystemPrompt(nil)
	result, err := f.HandleConfig("go.family_extra_system_prompt", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := result.(*FamilyExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *FamilyExtraSystemPrompt, got %T", result)
	}
	if len(*ret) != 2 {
		t.Fatalf("expected 2 families, got %d: %v", len(*ret), *ret)
	}
	if got := (*ret)["gemini"]; len(got) != 1 || got[0] != "gemini prompt" {
		t.Fatalf("unexpected gemini prompts: %v", got)
	}
	if got := (*ret)["deepseek"]; len(got) != 2 || got[0] != "deepseek one" || got[1] != "deepseek two" {
		t.Fatalf("unexpected deepseek prompts: %v", got)
	}
}

func TestFamilyExtraSystemPromptHandleConfigAccumulates(t *testing.T) {
	ctx := cuecontext.New()

	v1 := ctx.CompileString(`{gemini: "one"}`)
	v2 := ctx.CompileString(`{gemini: "two", deepseek: "three"}`)

	f := FamilyExtraSystemPrompt(nil)
	result, err := f.HandleConfig("go.family_extra_system_prompt", []*cue.Value{&v1})
	if err != nil {
		t.Fatal(err)
	}
	ret1 := result.(*FamilyExtraSystemPrompt)

	result2, err := ret1.HandleConfig("go.family_extra_system_prompt", []*cue.Value{&v2})
	if err != nil {
		t.Fatal(err)
	}
	ret2 := result2.(*FamilyExtraSystemPrompt)

	if got := (*ret2)["gemini"]; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("expected accumulated gemini prompts, got %v", got)
	}
	if got := (*ret2)["deepseek"]; len(got) != 1 || got[0] != "three" {
		t.Fatalf("unexpected deepseek prompts: %v", got)
	}
}
