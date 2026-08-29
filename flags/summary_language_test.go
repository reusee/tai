package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/reusee/dscope"
)

func TestSummaryLanguage(t *testing.T) {
	t.Run("Flag", func(t *testing.T) {
		scope := dscope.New(Module{})
		result, err := Parse(scope, []string{"-summary-language", "zh"})
		if err != nil {
			t.Fatal(err)
		}
		result.Call(func(language SummaryLanguage) {
			if string(language) != "zh" {
				t.Fatalf("expected zh, got %v", language)
			}
		})
	})

	t.Run("FlagNoArg", func(t *testing.T) {
		scope := dscope.New(Module{})
		if _, err := Parse(scope, []string{"-summary-language"}); err == nil {
			t.Fatal("expected error for summary-language with no argument")
		}
	})

	t.Run("Keys", func(t *testing.T) {
		if _, ok := SummaryLanguage("").Keys()["-summary-language"]; !ok {
			t.Fatal("-summary-language flag not registered in Keys()")
		}
	})

	t.Run("Config", func(t *testing.T) {
		ctx := cuecontext.New()
		v := ctx.CompileString(`"zh"`)
		def, err := SummaryLanguage("").HandleConfig("summary_language", []*cue.Value{&v})
		if err != nil {
			t.Fatal(err)
		}
		ret, ok := def.(*SummaryLanguage)
		if !ok {
			t.Fatalf("expected *SummaryLanguage, got %T", def)
		}
		if string(*ret) != "zh" {
			t.Fatalf("expected zh, got %v", *ret)
		}
	})
}
