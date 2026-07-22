package codes

import (
	"strings"
	"testing"
)

func TestPlanFlagDefault(t *testing.T) {
	if bool(planFlag) {
		t.Fatal("planFlag should default to false (planning disabled by default)")
	}
}

func TestSystemPromptPlan(t *testing.T) {
	module := Module{}

	t.Run("Disabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			DynamicContext(false),
			Apply(true),
			Plan(false),
			Shell(false),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
			ExtraSystemPrompt(""),
		)
		if strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must not include mandatory planning section when plan is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			DynamicContext(false),
			Apply(true),
			Plan(true),
			Shell(false),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must include mandatory planning section when plan is enabled")
		}
	})
}
