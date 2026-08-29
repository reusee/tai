package flags

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/reusee/dscope"
)

func TestSummaryLanguageFlag(t *testing.T) {
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
}

func TestSummaryLanguageFlagNoArg(t *testing.T) {
	scope := dscope.New(Module{})
	_, err := Parse(scope, []string{"-summary-language"})
	if err == nil {
		t.Fatal("expected error for summary-language with no argument")
	}
}

func TestSummaryLanguageKeys(t *testing.T) {
	keys := SummaryLanguage("").Keys()
	if _, ok := keys["-summary-language"]; !ok {
		t.Fatal("-summary-language flag not registered in Keys()")
	}
}

func TestSummaryLanguageConfig(t *testing.T) {
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
}
