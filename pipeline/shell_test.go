package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestShellBlockSystemPrompt(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		dscope.New(
			modes.ForTest(t),
			new(Module),
		).Fork(
			func() codetypes.PartsProvider { return mockPartsProvider{} },
		).Call(func(
			prompt SystemPrompt,
		) {
			if strings.Contains(string(prompt), "Shell Block Kind") {
				t.Fatal("system prompt must not include shell section when shell is disabled")
			}
			if !strings.Contains(string(prompt), "shell execution is disabled") {
				t.Fatal("system prompt should announce that shell blocks are disabled")
			}
		})
	})

	t.Run("Enabled", func(t *testing.T) {
		dscope.New(
			modes.ForTest(t),
			new(Module),
		).Fork(
			func() codetypes.PartsProvider { return mockPartsProvider{} },
			func() flags.Shell { return true },
		).Call(func(
			prompt SystemPrompt,
		) {
			if !strings.Contains(string(prompt), "Shell Block Kind") {
				t.Fatal("system prompt must include shell section when shell is enabled")
			}
			if strings.Contains(string(prompt), "shell execution is disabled") {
				t.Fatal("system prompt must not carry the disabled-shell notice when shell is enabled")
			}
		})
	})
}

func TestShellBlockSystemPromptAllowlist(t *testing.T) {
	// A configured command allowlist enables the shell section by itself
	// and switches it to the allowlist policy: the user who lists commands
	// has already decided to let the model run them, so requiring the
	// -shell flag as well would make the configuration silently inert. See
	// blocks.TheoryOfShellAllowlist.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
		func() blocks.AllowedShellCommands {
			return blocks.AllowedShellCommands{"git status", "ls -la"}
		},
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "Shell Block Kind") {
			t.Fatal("a configured allowlist must enable the shell section")
		}
		if strings.Contains(string(prompt), "shell execution is disabled") {
			t.Fatal("a configured allowlist must not carry the disabled-shell notice")
		}
		if !strings.Contains(string(prompt), "  - git status\n") {
			t.Fatal("the shell section must list the allowed commands")
		}
		if !strings.Contains(string(prompt), "  - ls -la\n") {
			t.Fatal("the shell section must list the allowed commands")
		}
	})
}
