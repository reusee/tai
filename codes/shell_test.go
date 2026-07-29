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
			mockCodeProvider{},
			flags.ExtraSystemPrompt(""),
			DynamicContext(false),
			flags.Apply(true),
			flags.Plan(false),
			flags.Shell(false),
			nil,
			nil,
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
			mockCodeProvider{},
			flags.ExtraSystemPrompt(""),
			DynamicContext(false),
			flags.Apply(true),
			flags.Plan(true),
			flags.Shell(true),
			nil,
			nil,
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
