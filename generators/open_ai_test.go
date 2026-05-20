package generators

import (
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
)

func TestOpenAI(t *testing.T) {
	testGenerator(t, func(
		newOpenRouter NewOpenRouter,
	) Generator {
		return newOpenRouter(Spec{
			Model:             "mistralai/devstral-2512:free",
			ContextTokens:     128 << 10,
			MaxGenerateTokens: new(8 << 10),
		})
	})
}

func TestStateToOpenAIMessages(t *testing.T) {

	t.Run("merge model messages separated by log messages", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleLog,
				Parts: []Part{
					Usage{},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("foo"),
				},
			},
			{
				Role: RoleLog,
				Parts: []Part{
					Usage{},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("bar"),
				},
			},
		})

		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}

		if len(messages) != 1 {
			t.Fatalf("got %+v", messages)
		}
		if messages[0].Content != "foobar" {
			t.Fatalf("got %+v", messages)
		}

	})

	t.Run("reasoning content", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			},
		})
		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 {
			t.Fatalf("got %+v", messages)
		}
		if messages[0].ReasoningContent != "thinking" {
			t.Errorf("wrong reasoning: %s", messages[0].ReasoningContent)
		}
		if messages[0].Content != "answer" {
			t.Errorf("wrong content: %s", messages[0].Content)
		}
	})

	t.Run("merge assistant messages with tool calls", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleModel,
				Parts: []Part{
					Text("thinking..."),
					FuncCall{ID: "1", Name: "foo", Arguments: map[string]any{}},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("more thinking..."),
				},
			},
		})
		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d: %+v", len(messages), messages)
		}
		if messages[0].Content != "thinking...more thinking..." {
			t.Errorf("wrong content: %s", messages[0].Content)
		}
		if len(messages[0].ToolCalls) != 1 {
			t.Errorf("wrong tool calls: %+v", messages[0].ToolCalls)
		}
	})

}

func TestAzureConfiguration(t *testing.T) {
	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		newAzure NewAzure,
	) {
		g := newAzure(Spec{
			BaseURL:    "https://foo.openai.azure.com/",
			Model:      "my-deployment",
			APIVersion: "2024-05-01-preview",
			APIKey:     "my-key",
		})
		if !g.spec.IsAzure {
			t.Fatal("IsAzure should be true")
		}
		if g.apiKey != "my-key" {
			t.Fatalf("wrong key: %s", g.apiKey)
		}
		if g.spec.APIVersion != "2024-05-01-preview" {
			t.Fatalf("wrong version: %s", g.spec.APIVersion)
		}
	})
}
