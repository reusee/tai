package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/flags"
)

func TestShellBlockSystemPrompt(t *testing.T) {
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
		if strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must not include shell section when shell is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		comps := module.CodesComponents(
			BoundaryDiffHandler{},
			mockCodeProvider{},
			ExtraSystemPrompt(""),
			DynamicContext(false),
			Apply(true),
			Plan(false),
			flags.Shell(true),
		)
		prompt := module.SystemPrompt(
			comps,
			mockCodeProvider{},
		)
		if !strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("system prompt must include shell section when shell is enabled")
		}
	})
}
