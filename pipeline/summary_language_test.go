package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestCodesComponentsSummaryLanguage(t *testing.T) {
	var defaultPrompt string
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(comps CodesComponents) {
		defaultPrompt = comps.PromptSections()
	})
	if strings.Contains(defaultPrompt, "Summary Language") {
		t.Fatal("no summary language configured must not produce a summary-language section")
	}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() flags.SummaryLanguage { return flags.SummaryLanguage("zh") },
	).Call(func(comps CodesComponents) {
		prompt := comps.PromptSections()
		if !strings.Contains(prompt, "Summary Language") {
			t.Fatal("expected summary-language section in system prompt")
		}
		if !strings.Contains(prompt, "in zh") {
			t.Fatal("expected the configured language in the system prompt")
		}
	})
}
