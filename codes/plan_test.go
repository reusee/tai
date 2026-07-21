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
		bindings := module.BlockBindings(
			BoundaryDiffHandler{},
			DynamicContext(false),
			Apply(true),
			Shell(false),
		)
		prompt := module.SystemPrompt(
			bindings,
			mockCodeProvider{},
			Plan(false),
			ExtraSystemPrompt(""),
		)
		if strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must not include mandatory planning section when plan is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		bindings := module.BlockBindings(
			BoundaryDiffHandler{},
			DynamicContext(false),
			Apply(true),
			Shell(false),
		)
		prompt := module.SystemPrompt(
			bindings,
			mockCodeProvider{},
			Plan(true),
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Mandatory Planning") {
			t.Fatal("system prompt must include mandatory planning section when plan is enabled")
		}
	})
}
