package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestSystemPromptPlan(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		dscope.New(
			modes.ForTest(t),
			new(Module),
		).Fork(
			func() codetypes.PartsProvider { return mockPartsProvider{} },
		).Call(func(
			prompt SystemPrompt,
		) {
			if strings.Contains(string(prompt), "Mandatory Planning") {
				t.Fatal("system prompt must not include mandatory planning section when plan is disabled")
			}
		})
	})

	t.Run("Enabled", func(t *testing.T) {
		dscope.New(
			modes.ForTest(t),
			new(Module),
		).Fork(
			func() codetypes.PartsProvider { return mockPartsProvider{} },
			func() flags.Plan { return true },
		).Call(func(
			prompt SystemPrompt,
		) {
			if !strings.Contains(string(prompt), "Mandatory Planning") {
				t.Fatal("system prompt must include mandatory planning section when plan is enabled")
			}
		})
	})
}
