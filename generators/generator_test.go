package generators

import (
	"encoding/json"
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
		for content := range prompts.Contents() {
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
		if call.Arguments["timezone"] != timezone {
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
		for content := range promptsState.Contents() {
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
		for content := range state.Contents() {
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
			return newGemini(Spec{
				Model: "models/gemini-flash-latest",
			})
		})
	})

	t.Run("openai", func(t *testing.T) {
		test(t, func(newOpenRouter NewOpenRouter) Generator {
			return newOpenRouter(Spec{
				Model: "mistralai/devstral-2512:free",
			})
		})
	})

}

func TestSpecNoProxy(t *testing.T) {
	spec := Spec{
		Name:    "test",
		Type:    "gemini",
		Model:   "gemini-flash",
		NoProxy: new(true),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["no_proxy"]; !ok || v != true {
		t.Errorf("no_proxy not found or wrong: %v", raw)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.NoProxy == nil || !*restored.NoProxy {
		t.Errorf("NoProxy not restored correctly: %+v", restored)
	}
}

func TestDeepseekTokenCounter(t *testing.T) {
	counter := DeepseekTokenCounterFn

	testCases := []struct {
		input  string
		expect int
	}{
		{"", 0},
		{"hello", 1},    // 5 * 0.3 = 1.5 → int 1
		{"世界", 1},       // 2 * 0.6 = 1.2 → int 1
		{"hello世界", 2},  // 1.5 + 1.2 = 2.7 → int 2
		{"hello 世界", 3}, // 6 * 0.3 + 2 * 0.6 = 3.0 → int 3
	}

	for _, tc := range testCases {
		got, err := counter(tc.input)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.expect {
			t.Errorf("for %q: got %d, expect %d", tc.input, got, tc.expect)
		}
	}
}

func TestResolveSpec(t *testing.T) {
	base := Spec{
		Name:          "base",
		Type:          "gemini",
		Model:         "base-model",
		ContextTokens: 100,
		Temperature:   new(float32(0.5)),
		DisableSearch: new(true),
		Variants: []Spec{
			{
				Name:              "variant1",
				Type:              "openai",
				APIKey:            "key1",
				MaxGenerateTokens: new(50),
				Aliases:           []string{"myvariant"},
				Variants: []Spec{
					{
						Name:          "sub",
						ContextTokens: 200,
						Temperature:   new(float32(0.8)),
					},
				},
			},
		},
	}
	roots := []Spec{base}

	t.Run("resolve base", func(t *testing.T) {
		s, err := resolveSpec("base", roots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base" {
			t.Errorf("expected name 'base', got %q", s.Name)
		}
		if s.Type != "gemini" {
			t.Errorf("expected type gemini, got %q", s.Type)
		}
		if s.Model != "base-model" {
			t.Errorf("expected model base-model, got %q", s.Model)
		}
		if s.ContextTokens != 100 {
			t.Errorf("expected context tokens 100, got %d", s.ContextTokens)
		}
		if s.Temperature == nil || *s.Temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %v", s.Temperature)
		}
		if s.MaxGenerateTokens != nil {
			t.Errorf("expected no max generate tokens, got %v", s.MaxGenerateTokens)
		}
		if s.DisableSearch == nil || !*s.DisableSearch {
			t.Errorf("expected disable search true")
		}
	})

	t.Run("resolve variant1", func(t *testing.T) {
		s, err := resolveSpec("base/variant1", roots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base/variant1" {
			t.Errorf("expected name 'base/variant1', got %q", s.Name)
		}
		if s.Type != "openai" {
			t.Errorf("expected type openai, got %q", s.Type)
		}
		if s.Model != "base-model" {
			t.Errorf("expected model base-model, got %q", s.Model)
		}
		if s.ContextTokens != 100 {
			t.Errorf("expected context tokens 100, got %d", s.ContextTokens)
		}
		if s.Temperature == nil || *s.Temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %v", s.Temperature)
		}
		if s.MaxGenerateTokens == nil || *s.MaxGenerateTokens != 50 {
			t.Errorf("expected max generate tokens 50, got %v", s.MaxGenerateTokens)
		}
		if s.APIKey != "key1" {
			t.Errorf("expected api key key1, got %q", s.APIKey)
		}
		if s.DisableSearch == nil || !*s.DisableSearch {
			t.Errorf("expected disable search true")
		}
	})

	t.Run("resolve sub", func(t *testing.T) {
		s, err := resolveSpec("base/variant1/sub", roots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base/variant1/sub" {
			t.Errorf("expected name 'base/variant1/sub', got %q", s.Name)
		}
		if s.Type != "openai" {
			t.Errorf("expected type openai, got %q", s.Type)
		}
		if s.Model != "base-model" {
			t.Errorf("expected model base-model, got %q", s.Model)
		}
		if s.ContextTokens != 200 {
			t.Errorf("expected context tokens 200, got %d", s.ContextTokens)
		}
		if s.Temperature == nil || *s.Temperature != 0.8 {
			t.Errorf("expected temperature 0.8, got %v", s.Temperature)
		}
		if s.APIKey != "key1" {
			t.Errorf("expected api key key1, got %q", s.APIKey)
		}
		if s.MaxGenerateTokens == nil || *s.MaxGenerateTokens != 50 {
			t.Errorf("expected max generate tokens 50, got %v", s.MaxGenerateTokens)
		}
		if s.DisableSearch == nil || !*s.DisableSearch {
			t.Errorf("expected disable search true")
		}
	})

	t.Run("override disable search to false", func(t *testing.T) {
		localRoots := []Spec{
			{
				Name:          "base",
				Type:          "gemini",
				DisableSearch: new(true),
				Variants: []Spec{
					{
						Name:          "variant",
						Type:          "openai",
						DisableSearch: new(false),
					},
				},
			},
		}
		s, err := resolveSpec("base/variant", localRoots)
		if err != nil {
			t.Fatal(err)
		}
		if s.DisableSearch == nil || *s.DisableSearch != false {
			t.Errorf("expected disable search false, got %v", s.DisableSearch)
		}
	})

	t.Run("resolve alias", func(t *testing.T) {
		localRoots := []Spec{
			{
				Name:    "base",
				Type:    "gemini",
				Aliases: []string{"mybase"},
			},
		}
		s, err := resolveSpec("mybase", localRoots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base" {
			t.Errorf("expected name 'base', got %q", s.Name)
		}
		if s.Type != "gemini" {
			t.Errorf("expected type gemini, got %q", s.Type)
		}
	})

	t.Run("resolve alias of variant", func(t *testing.T) {
		s, err := resolveSpec("myvariant", roots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base/variant1" {
			t.Errorf("expected name 'base/variant1', got %q", s.Name)
		}
		if s.Type != "openai" {
			t.Errorf("expected type openai, got %q", s.Type)
		}
	})

	t.Run("resolve alias as path element", func(t *testing.T) {
		s, err := resolveSpec("base/myvariant/sub", roots)
		if err != nil {
			t.Fatal(err)
		}
		if s.Name != "base/myvariant/sub" {
			t.Errorf("expected name 'base/myvariant/sub', got %q", s.Name)
		}
		if s.Model != "base-model" {
			t.Errorf("expected model base-model, got %q", s.Model)
		}
		if s.ContextTokens != 200 {
			t.Errorf("expected context tokens 200, got %d", s.ContextTokens)
		}
		if s.Temperature == nil || *s.Temperature != 0.8 {
			t.Errorf("expected temperature 0.8, got %v", s.Temperature)
		}
		if s.MaxGenerateTokens == nil || *s.MaxGenerateTokens != 50 {
			t.Errorf("expected max generate tokens 50, got %v", s.MaxGenerateTokens)
		}
	})

	t.Run("resolve not found", func(t *testing.T) {
		_, err := resolveSpec("nonexistent", roots)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("resolve empty name", func(t *testing.T) {
		_, err := resolveSpec("/", roots)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}