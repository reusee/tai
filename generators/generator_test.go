package generators

import (
	"os"
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
		state, err = generator.Generate(t.Context(), state)
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

		_, err = generator.Generate(t.Context(), state)
		if err != nil {
			t.Fatal(err)
		}

	})

}
