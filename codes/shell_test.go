package codes

import (
	"strings"
	"testing"
)

func TestShellBlockSystemPrompt(t *testing.T) {
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
		if strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must not include shell section when shell is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			DynamicContext(false),
			Apply(true),
			Plan(false),
			Shell(true),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must include shell section when shell is enabled")
		}
	})
}

func TestShellFlagDefault(t *testing.T) {
	if bool(shellFlag) {
		t.Fatal("shellFlag should default to false (disabled by default)")
	}
}
