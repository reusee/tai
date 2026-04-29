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
		return newOpenRouter(GeneratorArgs{
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

}

func TestAzureConfiguration(t *testing.T) {
	dscope.New(
		new(Module),
		modes.ForTest(t),
	).Call(func(
		newAzure NewAzure,
	) {
		g := newAzure(GeneratorArgs{
			BaseURL:    "https://foo.openai.azure.com/",
			Model:      "my-deployment",
			APIVersion: "2024-05-01-preview",
			APIKey:     "my-key",
		})
		if !g.args.IsAzure {
			t.Fatal("IsAzure should be true")
		}
		if g.apiKey != "my-key" {
			t.Fatalf("wrong key: %s", g.apiKey)
		}
		if g.args.APIVersion != "2024-05-01-preview" {
			t.Fatalf("wrong version: %s", g.args.APIVersion)
		}
	})
}

