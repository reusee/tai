package generators

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
)

func testGenerator(
	t *testing.T,
	newGenerator any,
) {

	t.Run("func call", func(t *testing.T) {
		loader := configs.NewLoader([]string{}, "")
		scope := dscope.New(
			modes.ForTest(t),
			&loader,
			new(Module),
		).Fork(
			func() nets.ProxyAddr {
				return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
			},
		)

		var generator Generator
		scope.Call(newGenerator).Assign(&generator)

		timezone := "Asia/Hong_Kong"
		prompts := NewPrompts("", []*Content{
			{
				Role: RoleUser,
				Parts: []Part{
					Text("what time is it? timezone is " + timezone),
				},
			},
		})
		funcMap := NewFuncMap(prompts, FuncNow)
		output := NewOutput(funcMap, t.Output(), true)
		state := State(output)

		var err error
		state, err = generator.Generate(t.Context(), state, nil)
		if err != nil {
			t.Fatal(err)
		}

		var calls []FuncCall
		prompts, ok := As[Prompts](state)
		if !ok {
			t.Fatal("Prompts not found")
		}
		for _, content := range prompts.Contents() {
			for _, part := range content.Parts {
				if call, ok := part.(FuncCall); ok {
					calls = append(calls, call)
				}
			}
		}
		if len(calls) != 1 {
			t.Fatalf("got %v", calls)
		}

		call := calls[0]
		if call.Name != "now" {
			t.Fatalf("got %+v", call)
		}
		if call.Args["timezone"] != timezone {
			t.Fatalf("got %+v", call)
		}

		location, err := time.LoadLocation(timezone)
		if err != nil {
			t.Fatal()
		}
		now := time.Now().In(location).Format(time.RFC3339)
		state, err = state.AppendContent(&Content{
			Role: RoleTool,
			Parts: []Part{
				CallResult{
					ID:   call.ID,
					Name: call.Name,
					Results: map[string]any{
						"now": now,
					},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}

		_, err = generator.Generate(t.Context(), state, nil)
		if err != nil {
			t.Fatal(err)
		}

	})

	t.Run("structured output", func(t *testing.T) {
		loader := configs.NewLoader([]string{}, "")
		scope := dscope.New(
			modes.ForTest(t),
			&loader,
			new(Module),
		).Fork(
			func() nets.ProxyAddr {
				return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
			},
		)

		var generator Generator
		scope.Call(newGenerator).Assign(&generator)

		schema := &Var{
			Type: TypeObject,
			Properties: Vars{
				{
					Name: "answer",
					Type: TypeString,
				},
			},
		}

		prompts := NewPrompts("", []*Content{
			{
				Role: RoleUser,
				Parts: []Part{
					Text("say hello in json"),
				},
			},
		})
		output := NewOutput(prompts, t.Output(), true)
		state := State(output)

		var err error
		state, err = generator.Generate(t.Context(), state, &GenerateOptions{
			ResponseSchema: schema,
		})
		if err != nil {
			t.Fatal(err)
		}

		promptsState, ok := As[Prompts](state)
		if !ok {
			t.Fatal("Prompts not found")
		}

		found := false
		for _, content := range promptsState.Contents() {
			if content.Role != RoleModel && content.Role != RoleAssistant {
				continue
			}
			for _, part := range content.Parts {
				if text, ok := part.(Text); ok {
					if strings.Contains(string(text), `"answer"`) {
						found = true
						break
					}
				}
			}
		}
		if !found {
			t.Errorf("structured output not found in result")
		}

	})

}

func TestNonStreaming(t *testing.T) {
	test := func(t *testing.T, newGenerator any) {
		loader := configs.NewLoader([]string{}, "")
		scope := dscope.New(
			modes.ForTest(t),
			&loader,
			new(Module),
		).Fork(
			func() nets.ProxyAddr {
				return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
			},
		)

		var generator Generator
		scope.Call(newGenerator).Assign(&generator)

		prompts := NewPrompts("", []*Content{
			{
				Role: RoleUser,
				Parts: []Part{
					Text("say hi"),
				},
			},
		})
		output := NewOutput(prompts, t.Output(), true)
		state := State(output)

		var err error
		state, err = generator.Generate(t.Context(), state, &GenerateOptions{
			NonStreaming: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, content := range state.Contents() {
			if content.Role != RoleModel && content.Role != RoleAssistant {
				continue
			}
			for _, part := range content.Parts {
				if text, ok := part.(Text); ok && len(text) > 0 {
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatal("no response content")
		}
	}

	t.Run("gemini", func(t *testing.T) {
		test(t, func(newGemini NewGemini) Generator {
			return newGemini(GeneratorArgs{
				Model: "models/gemini-flash-latest",
			})
		})
	})

	t.Run("openai", func(t *testing.T) {
		test(t, func(newOpenRouter NewOpenRouter) Generator {
			return newOpenRouter(GeneratorArgs{
				Model: "mistralai/devstral-2512:free",
			})
		})
	})

}