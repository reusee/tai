package records

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/tree"
)

type analysisMockGenerator struct{}

func (analysisMockGenerator) Spec() generators.Spec {
	return generators.Spec{Model: "test-model"}
}

func (analysisMockGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (analysisMockGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	return state.AppendContent(&generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Text("analysis report output"),
		},
	})
}

func TestRunAnalysis(t *testing.T) {
	dscope.New(
		modes.ForTest(t),
		new(Module),
		// RunAnalysis provider 的依赖必须在 new(Module) 同一层已定义，
		// 否则 dscope.New 校验 records.Module 时会因缺少
		// generators.GetDefaultGenerator / generators.BuildGenerate 而 panic。
		func() generators.GetDefaultGenerator {
			return func() (generators.Generator, error) {
				return analysisMockGenerator{}, nil
			}
		},
		func() generators.BuildGenerate {
			// The stub's phase must invoke generator.Generate, mirroring
			// generators.BuildGenerate: runAnalysis drives the returned phase
			// chain, and the mock generator's output reaches the buffer
			// through the Output state layer only when Generate runs.
			return func(generator generators.Generator, options *generators.GenerateOptions) generators.PhaseBuilder {
				return func(cont generators.Phase) generators.Phase {
					return func(ctx context.Context, state generators.State) (generators.Phase, generators.State, error) {
						newState, err := generator.Generate(ctx, state, options)
						if err != nil {
							return nil, state, err
						}
						return cont, newState, nil
					}
				}
			}
		},
	).Fork(
		// 覆盖 defs 位于独立 Fork 层，避免与 new(Module) 同层重复定义。
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		// Recording must be enabled: with the default disabled state,
		// StartSession is a no-op and no session row is inserted,
		// causing the query below to fail with "sql: no rows in result
		// set". This test records a session to analyze, so it must
		// enable recording. See TheoryOfInteractionRecording.
		func() Enabled {
			return Enabled(true)
		},
	).Call(func(recorder *Recorder, runAnalysis RunAnalysis) {
		recorder.StartSession("test")
		tr := tree.New().WithOpSink(recorder.Sink())
		if _, err := tr.Write("root", "user-1", tree.TypeUser, tree.AuthorUser, "task input"); err != nil {
			t.Fatal(err)
		}
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := runAnalysis(context.Background(), id, &buf); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "analysis report output") {
			t.Fatalf("expected analysis output, got: %s", buf.String())
		}
	})
}

func TestAnalysisSystemPromptContent(t *testing.T) {
	for _, want := range []string{
		"交互概要",
		"问题清单",
		"根因分析",
		"改进建议",
		"尝试",
		"循环",
		"事件流",
		"kind=delete",
	} {
		if !strings.Contains(analysisSystemPrompt, want) {
			t.Fatalf("analysisSystemPrompt missing %q", want)
		}
	}
}
