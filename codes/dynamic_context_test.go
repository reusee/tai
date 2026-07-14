package codes

import (
	"strings"
	"testing"

	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
)

type mockCodeProvider struct{}

var _ codetypes.CodeProvider = mockCodeProvider{}

func (mockCodeProvider) Parts(int, func(string) (int, error), []string) ([]generators.Part, error) {
	return nil, nil
}

func (mockCodeProvider) Functions() []*generators.Function {
	return nil
}

func (mockCodeProvider) SystemPrompt() string {
	return ""
}

func TestSystemPromptDynamicContext(t *testing.T) {
	module := Module{}

	t.Run("Disabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(false),
			ExtraSystemPrompt(""),
		)
		if strings.Contains(string(prompt), "Request-Context Block Kind") {
			t.Fatal("system prompt must not include request-context section when dynamic context is disabled")
		}
	})

	t.Run("Enabled", func(t *testing.T) {
		prompt := module.SystemPrompt(
			mockCodeProvider{},
			BoundaryDiffHandler{},
			DynamicContext(true),
			ExtraSystemPrompt(""),
		)
		if !strings.Contains(string(prompt), "Request-Context Block Kind") {
			t.Fatal("system prompt must include request-context section when dynamic context is enabled")
		}
	})
}

func TestSystemPromptReadOnlyFiles(t *testing.T) {
	module := Module{}
	prompt := module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		ExtraSystemPrompt(""),
	)
	if !strings.Contains(string(prompt), "Read-Only Files") {
		t.Fatal("system prompt must include the read-only files section")
	}
	if !strings.Contains(string(prompt), "read-only") {
		t.Fatal("system prompt must reference read-only files")
	}
}

func TestSystemPromptContinueBlock(t *testing.T) {
	module := Module{}
	prompt := module.SystemPrompt(
		mockCodeProvider{},
		BoundaryDiffHandler{},
		DynamicContext(false),
		ExtraSystemPrompt(""),
	)
	if !strings.Contains(string(prompt), "Continue Block Kind") {
		t.Fatal("system prompt must include continue block section")
	}
	if !strings.Contains(string(prompt), ":::continue") {
		t.Fatal("system prompt must include continue block format")
	}
	if !strings.Contains(string(prompt), "Task Decomposition") {
		t.Fatal("system prompt must include task decomposition strategy for complex tasks")
	}
}
