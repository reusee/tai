package flags

import (
	"slices"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestExtraSystemPromptHandleConfigTableDriven covers string aggregation,
// list aggregation, mixed string-and-list values, empty-string skipping,
// and cross-call accumulation in one table.
func TestExtraSystemPromptHandleConfigTableDriven(t *testing.T) {
	for _, tt := range []struct {
		name string
		cues []string
		want []string
	}{
		{name: "strings", cues: []string{`"prompt1"`, `"prompt2"`}, want: []string{"prompt1", "prompt2"}},
		{name: "list", cues: []string{`["prompt1", "prompt2", "prompt3"]`}, want: []string{"prompt1", "prompt2", "prompt3"}},
		{name: "mixed", cues: []string{`"single"`, `["list1", "list2"]`}, want: []string{"single", "list1", "list2"}},
		{name: "empty skipped", cues: []string{`""`}, want: []string{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := cuecontext.New()
			values := make([]*cue.Value, 0, len(tt.cues))
			for _, src := range tt.cues {
				v := ctx.CompileString(src)
				values = append(values, &v)
			}
			e := ExtraSystemPrompt(nil)
			result, err := e.HandleConfig("extra_system_prompt", values)
			if err != nil {
				t.Fatal(err)
			}
			ret, ok := result.(*ExtraSystemPrompt)
			if !ok {
				t.Fatalf("expected *ExtraSystemPrompt, got %T", result)
			}
			if !slices.Equal(*ret, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, *ret)
			}
		})
	}

	// Accumulation across HandleConfig calls: the result of one call is
	// the receiver of the next.
	ctx := cuecontext.New()
	v1 := ctx.CompileString(`"prompt1"`)
	e := ExtraSystemPrompt(nil)
	result, err := e.HandleConfig("extra_system_prompt", []*cue.Value{&v1})
	if err != nil {
		t.Fatal(err)
	}
	ret1 := result.(*ExtraSystemPrompt)
	v2 := ctx.CompileString(`"prompt2"`)
	result2, err := ret1.HandleConfig("extra_system_prompt", []*cue.Value{&v2})
	if err != nil {
		t.Fatal(err)
	}
	ret2 := result2.(*ExtraSystemPrompt)
	if !slices.Equal(*ret2, []string{"prompt1", "prompt2"}) {
		t.Fatalf("expected accumulated [prompt1 prompt2], got %v", *ret2)
	}
}

// TestFamilyExtraSystemPromptHandleConfigTableDriven covers mixed
// string-and-list family values and cross-call accumulation in one test.
func TestFamilyExtraSystemPromptHandleConfigTableDriven(t *testing.T) {
	ctx := cuecontext.New()

	// Mixed string and list values per family.
	v := ctx.CompileString(`{
		gemini: "gemini prompt"
		deepseek: ["deepseek one", "deepseek two"]
	}`)
	f := FamilyExtraSystemPrompt(nil)
	result, err := f.HandleConfig("family_extra_system_prompt", []*cue.Value{&v})
	if err != nil {
		t.Fatal(err)
	}
	ret, ok := result.(*FamilyExtraSystemPrompt)
	if !ok {
		t.Fatalf("expected *FamilyExtraSystemPrompt, got %T", result)
	}
	if got := (*ret)["gemini"]; !slices.Equal(got, []string{"gemini prompt"}) {
		t.Fatalf("unexpected gemini prompts: %v", got)
	}
	if got := (*ret)["deepseek"]; !slices.Equal(got, []string{"deepseek one", "deepseek two"}) {
		t.Fatalf("unexpected deepseek prompts: %v", got)
	}

	// Accumulation across calls: the same family accumulates, a new
	// family is added.
	v1 := ctx.CompileString(`{gemini: "one"}`)
	v2 := ctx.CompileString(`{gemini: "two", deepseek: "three"}`)
	f = FamilyExtraSystemPrompt(nil)
	result, err = f.HandleConfig("family_extra_system_prompt", []*cue.Value{&v1})
	if err != nil {
		t.Fatal(err)
	}
	ret1 := result.(*FamilyExtraSystemPrompt)
	result2, err := ret1.HandleConfig("family_extra_system_prompt", []*cue.Value{&v2})
	if err != nil {
		t.Fatal(err)
	}
	ret2 := result2.(*FamilyExtraSystemPrompt)
	if got := (*ret2)["gemini"]; !slices.Equal(got, []string{"one", "two"}) {
		t.Fatalf("expected accumulated gemini prompts, got %v", got)
	}
	if got := (*ret2)["deepseek"]; !slices.Equal(got, []string{"three"}) {
		t.Fatalf("unexpected deepseek prompts: %v", got)
	}
}
