package generators

import (
	"testing"

	"github.com/reusee/tai/vars"
)

func TestOpenAI(t *testing.T) {
	testGenerator(t, func(
		new NewOpenRouter,
	) Generator {
		return new(GeneratorArgs{
			Model:             "mistralai/devstral-2512:free",
			ContextTokens:     128 << 10,
			MaxGenerateTokens: vars.PtrTo(8 << 10),
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
