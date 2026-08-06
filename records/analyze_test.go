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
	"github.com/reusee/tai/phases"
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
	).Fork(
		func() DBPath {
			return DBPath(filepath.Join(t.TempDir(), "test.db"))
		},
		// Recording must be enabled: with the default disabled state,
		// StartSession is a no-op and no session row is inserted,
		// causing the query below to fail with "sql: no rows in result
		// set". Other tests in this package enable recording via the
		// withRecorder helper; this test records an interaction to
		// analyze, so it must enable recording too.
		// See TheoryOfInteractionRecording.
		func() Enabled {
			return Enabled(true)
		},
	).Call(func(recorder *Recorder) {
		recorder.StartSession("test")
		recorder.RoundStart()
		recorder.RoundSuccess(nil)
		recorder.EndSession(nil)

		var id int64
		if err := recorder.db.QueryRow(`SELECT id FROM sessions LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}

		buildGenerate := phases.BuildGenerate(func(generator generators.Generator, options *generators.GenerateOptions) phases.PhaseBuilder {
			return func(cont phases.Phase) phases.Phase {
				return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
					newState, err := generator.Generate(ctx, state, options)
					if err != nil {
						return nil, state, err
					}
					return nil, newState, nil
				}
			}
		})

		var buf bytes.Buffer
		err := RunAnalysis(
			context.Background(),
			analysisMockGenerator{},
			buildGenerate,
			recorder,
			id,
			&buf,
		)
		if err != nil {
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
		"轮次",
	} {
		if !strings.Contains(analysisSystemPrompt, want) {
			t.Fatalf("analysisSystemPrompt missing %q", want)
		}
	}
}
