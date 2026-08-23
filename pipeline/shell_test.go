package pipeline

import (
	"strings"
	"testing"

	"github.com/reusee/dscope"
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
			func() flags.Plan { return true },
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
