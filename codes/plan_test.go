package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/flags"
)

func TestSystemPromptPlan(t *testing.T) {
	module := Module{}

	t.Run("Disabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			mockCodeProvider{},
			ExtraSystemPrompt(""),
			DynamicContext(false),
			Apply(true),
			Plan(false),
			flags.Shell(false),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
		)
		if strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must not include mandatory planning section when plan is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			mockCodeProvider{},
			ExtraSystemPrompt(""),
			DynamicContext(false),
			Apply(true),
			Plan(true),
			flags.Shell(false),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
		)
		if !strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must include mandatory planning section when plan is enabled")
		}
	})
}
