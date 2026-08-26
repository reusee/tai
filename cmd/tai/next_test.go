package main

import (
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
)

func TestSystemPrompt(t *testing.T) {
	t.Skip()

	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() nets.ProxyAddr {
			return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
		},
		new(flags.ModelName("deepseek-flash")),
	).Call(func(
		getDefaultGenerator generators.GetDefaultGenerator,
		systemPrompt SystemPrompt,
	) {
		generator, err := getDefaultGenerator()
		ce(err)

		t.Run("English", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`What language I am using?`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, true)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "English") &&
				!strings.Contains(output, "英语") {
				t.Fatalf("got %s", output)
			}
		})

		t.Run("Chinese", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`我用的是什么语言？`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, true)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "中文") {
				t.Fatalf("got %s", output)
			}
		})

		t.Run("Cantonese", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`我用嘅喺乜语言？`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, true)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "粤") &&
				!strings.Contains(output, "粵語") &&
				!strings.Contains(output, "廣東話") {
				t.Fatalf("got %s", output)
			}
		})

		t.Run("Style", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`汝可助吾一臂之力否`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, true)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "汝") &&
				!strings.Contains(output, "吾") &&
				!strings.Contains(output, "君") &&
				!strings.Contains(output, "也") {
				t.Fatalf("got %s", output)
			}
		})

		t.Run("Who you are", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`详细说明你是什么，你怎么做这些事情，有何规则。`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, false)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}

			output := strings.ToLower(buf.String())
			t.Logf("%s", output)
			forbiddenKeywords := []string{
				"ai助手", "使命", "思维框架",
				"结构化思考", "差距分析", "system prompt",
				"define goal", "assess current state", "gap analysis",
			}
			for _, keyword := range forbiddenKeywords {
				if strings.Contains(output, keyword) {
					t.Fatalf("output should not contain keyword '%s', but got: %s", keyword, output)
				}
			}
			if strings.Contains(output, "* ") || strings.Contains(output, "##") {
				t.Fatalf("output should not contain markdown list or header, but got: %s", output)
			}
		})

		t.Run("Wrong", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text(`我想不靠氧气罐下潜到马里亚纳海沟底部`),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, false)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "无法") &&
				!strings.Contains(output, "不可能") {
				t.Fatalf("got %s", output)
			}
		})

		t.Run("Focus with @@ai", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text("这是一个关于A的文档，内容是A1, A2, A3。"),
							generators.Text("这是另一个关于B的文档，内容是B1, B2, B3。"),
							generators.Text("@@ai 我应该如何处理C？"),
							generators.Text("这是关于D的文档，内容是D1, D2, D3。"),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, false)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			if !strings.Contains(output, "C") {
				t.Fatalf("output should focus on 'C', but got: %s", output)
			}
			if strings.Contains(output, "A1") || strings.Contains(output, "B1") || strings.Contains(output, "D1") {
				t.Fatalf("output should ignore content not marked by @@ai, but got: %s", output)
			}
		})

		t.Run("Focus with multiple @@ai tags", func(t *testing.T) {
			buf := new(strings.Builder)
			var state generators.State
			state = generators.NewPrompts(
				string(systemPrompt),
				[]*generators.Content{
					{
						Role: "user",
						Parts: []generators.Part{
							generators.Text("@@ai 任务一"),
							generators.Text("@@ai 任务二"),
						},
					},
				},
			)
			state = generators.NewOutput(state, buf, false)

			_, err := generator.Generate(t.Context(), state, nil)
			if err != nil {
				t.Fatal(err)
			}
			output := buf.String()
			t.Logf("%s", output)
			keywords := []string{"标记"}
			for _, keyword := range keywords {
				if !strings.Contains(output, keyword) {
					t.Fatalf("output should report multiple @@ai tags, but got: %s", output)
				}
			}
		})

	})
}

func TestExtraSystemPrompt(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() flags.ExtraSystemPrompt {
			return flags.ExtraSystemPrompt{"THIS_IS_EXTRA_SYSTEM_PROMPT"}
		},
	).Call(func(
		systemPrompt SystemPrompt,
	) {
		if !strings.Contains(string(systemPrompt), "THIS_IS_EXTRA_SYSTEM_PROMPT") {
			t.Fatalf("extra prompt not included in system prompt: %s", systemPrompt)
		}
	})
}

func TestSystemPromptAndUserPromptChangeBlockPlacement(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	// General-purpose tools must support file editing capabilities (change blocks)
	// for any type of file, not exclusively Go files.
	if err := os.WriteFile("test.md", []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(
		systemPrompt SystemPrompt,
		userPrompt UserPrompt,
	) {
		s := string(systemPrompt)
		if !strings.Contains(s, "Change Block Kind") {
			t.Fatal("system prompt must include change block prompt when focus files are present")
		}
		// The user prompt ends with the verbatim system prompt restate,
		// so the change block rules — including the precise-modification
		// guidance — are re-read right before generating. See
		// components.TheoryOfComponents.
		if len(userPrompt) == 0 {
			t.Fatal("user prompt must have parts")
		}
		last := userPrompt[len(userPrompt)-1]
		text, ok := last.(generators.Text)
		if !ok || text != components.SystemPromptRestate(s) {
			t.Fatalf("user prompt must end with the verbatim system prompt restate, got %T", last)
		}
		if !strings.Contains(string(text), "Prefer Precise Modifications") {
			t.Fatal("the restate must carry the change block prompt's guidance")
		}
	})
}

func TestUserPromptEndsWithSystemPromptRestate(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("test.md", []byte("# Title\n"), 0644); err != nil {
		t.Fatal(err)
	}

	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return aiMockGenerator{}, nil
			}
		},
	).Call(func(
		userPrompt UserPrompt,
		systemPrompt SystemPrompt,
	) {
		// The user prompt must end with the verbatim system prompt
		// restate: the full system prompt repeated under a short re-read
		// instruction, so the model re-reads every rule immediately
		// before generating and the reminder can never drift out of sync
		// with the instructions. See components.TheoryOfComponents.
		if len(userPrompt) == 0 {
			t.Fatal("user prompt must have parts")
		}
		last := userPrompt[len(userPrompt)-1]
		text, ok := last.(generators.Text)
		if !ok {
			t.Fatalf("last user prompt part must be a text part, got %T", last)
		}
		if want := components.SystemPromptRestate(string(systemPrompt)); text != want {
			t.Fatal("user prompt must end with the verbatim system prompt restate")
		}
	})
}

// TestUserPromptFilePatternsDeterministic verifies that the file patterns
// passed to the parts provider are sorted: flags.Files is a map, and Go
// map iteration order is randomized per range. IterFiles deduplicates
// followed symlink targets by first-wins, so with two symlink aliases of
// one directory the unsorted pattern order decides which alias path
// reaches the prompt. Resolving UserPrompt repeatedly from one map must
// produce byte-identical file parts, or the LLM prefix cache is
// invalidated run to run.
func TestUserPromptFilePatternsDeterministic(t *testing.T) {
	dir := t.TempDir()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Two symlink aliases of one shared directory: whichever alias
	// IterFiles dequeues first is followed; the other is skipped by the
	// visited-symlinks set, so the pattern order picks the emitted path.
	if err := os.Mkdir("target", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("target/notes.md", []byte("# Notes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", "aliasA"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("target", "aliasB"); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	flagFiles := flags.Files{
		"aliasA": true,
		"aliasB": true,
	}

	// Each resolution ranges over the same map afresh, so unsorted keys
	// produce a different pattern order — and different prompt bytes —
	// across resolutions with overwhelming probability. Sorted keys
	// always pick aliasA ("aliasA" < "aliasB"), so every resolution is
	// byte-identical. The generator is wrapped because aiMockGenerator
	// carries zero ContextTokens, which would leave the parts provider a
	// negative token budget and skip every file.
	var want string
	for i := 0; i < 16; i++ {
		var got string
		dscope.New(
			new(Module),
		).Fork(
			modes.ForTest(t),
			func() generators.GetDefaultGenerator {
				return func() (generators.Generator, error) {
					return userPromptMockGenerator{}, nil
				}
			},
			func() flags.Files {
				return flagFiles
			},
		).Call(func(
			userPrompt UserPrompt,
		) {
			var sb strings.Builder
			for _, part := range userPrompt {
				if text, ok := part.(generators.Text); ok {
					sb.WriteString(string(text))
				}
			}
			got = sb.String()
		})
		if i == 0 {
			want = got
			if !strings.Contains(want, "aliasA/notes.md") {
				t.Fatalf("expected the sorted-first alias in the prompt, got:\n%s", want)
			}
			if strings.Contains(want, "aliasB/notes.md") {
				t.Fatalf("expected the second alias to be deduplicated, got:\n%s", want)
			}
			continue
		}
		if got != want {
			t.Fatalf("user prompt differs across resolutions with equal configuration;\nfirst:\n%s\nlater:\n%s", want, got)
		}
	}
}

// userPromptMockGenerator adapts aiMockGenerator with a realistic context
// window: the parts provider skips every file when the token budget is
// non-positive, so tests asserting on file parts need a positive
// ContextTokens.
type userPromptMockGenerator struct {
	aiMockGenerator
}

func (userPromptMockGenerator) Spec() generators.Spec {
	maxGenerate := 1024
	return generators.Spec{
		ContextTokens:     1 << 20,
		MaxGenerateTokens: &maxGenerate,
	}
}

func TestSystemPromptIgnoreOrderDeterministic(t *testing.T) {
	// The ignore section derives from a map, and maps.Keys iteration order
	// is non-deterministic. The SystemPrompt must sort ignore items so the
	// system prompt is byte-identical across runs with equal configuration,
	// preserving the LLM prefix cache. This test fails (with high
	// probability) when the sort is removed. See TheoryOfPrefixCaching in
	// generators/state_func_map.go.
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() flags.Files {
			// A pattern that matches nothing, so HasFiles is false and no
			// change-block prompt is included; the test only checks the
			// ignore section ordering.
			return flags.Files{"/nonexistent-prefix-cache-test": true}
		},
		func() flags.Ignore {
			return flags.Ignore{"bbb": true, "aaa": true, "ccc": true}
		},
	).Call(func(systemPrompt SystemPrompt) {
		s := string(systemPrompt)
		sectionStart := strings.Index(s, "忽略这些方面：")
		if sectionStart == -1 {
			t.Fatal("ignore section not found in system prompt")
		}
		section := s[sectionStart:]
		aaaIdx := strings.Index(section, "\n- aaa\n")
		bbbIdx := strings.Index(section, "\n- bbb\n")
		cccIdx := strings.Index(section, "\n- ccc\n")
		if aaaIdx == -1 || bbbIdx == -1 || cccIdx == -1 {
			t.Fatalf("ignore items not found in system prompt: %s", s)
		}
		if !(aaaIdx < bbbIdx && bbbIdx < cccIdx) {
			t.Fatalf("ignore items must be sorted for prompt determinism: %s", s)
		}
	})
}

func TestSystemPromptIncludesFamilyExtraSystemPrompt(t *testing.T) {
	dscope.New(
		new(Module),
	).Fork(
		modes.ForTest(t),
		func() generators.ModelFamily { return "gemini" },
		func() flags.FamilyExtraSystemPrompt {
			return flags.FamilyExtraSystemPrompt{"gemini": {"gemini family prompt"}}
		},
	).Call(func(systemPrompt SystemPrompt) {
		if !strings.Contains(string(systemPrompt), "gemini family prompt") {
			t.Fatal("expected family prompt in next system prompt")
		}
	})
}
