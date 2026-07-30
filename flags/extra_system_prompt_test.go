package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func TestExtraSystemPromptHandleConfigAggregatesStrings(t *testing.T) {
	ctx := cuecontext.New()

	v1 := ctx.CompileString(`"prompt1"`)
	v2 := ctx.CompileString(`"prompt2"`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v1, &v2})
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
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v})
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

func TestExtraSystemPromptHandleConfigAccumulatesAcrossCalls(t *testing.T) {
	ctx := cuecontext.New()

	// Simulate the first HandleConfig call (e.g., from first config root)
	v1 := ctx.CompileString(`"prompt1"`)
	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v1})
	if err != nil {
		t.Fatal(err)
	}
	ret1, ok := result.(*ExtraSystemPrompt)
	if !ok {
		t.Fatal("expected *ExtraSystemPrompt")
	}

	// Simulate a second HandleConfig call with the receiver from the first call
	v2 := ctx.CompileString(`"prompt2"`)
	result2, err := ret1.HandleConfig("extra_system_prompt", []*cue.Value{&v2})
	if err != nil {
		t.Fatal(err)
	}
	ret2, ok := result2.(*ExtraSystemPrompt)
	if !ok {
		t.Fatal("expected *ExtraSystemPrompt")
	}

	if len(*ret2) != 2 {
		t.Fatalf("expected 2 prompts after accumulation, got %d: %v", len(*ret2), *ret2)
	}
	if (*ret2)[0] != "prompt1" || (*ret2)[1] != "prompt2" {
		t.Fatalf("expected [prompt1, prompt2], got %v", *ret2)
	}
}

func TestExtraSystemPromptHandleConfigMixedStringAndList(t *testing.T) {
	ctx := cuecontext.New()

	v1 := ctx.CompileString(`"single"`)
	v2 := ctx.CompileString(`["list1", "list2"]`)

	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v1, &v2})
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
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v})
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
