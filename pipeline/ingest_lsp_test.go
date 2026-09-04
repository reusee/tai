package pipeline

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/pipeline/codetypes"
)

func TestSystemPromptIncludesLSPTag(t *testing.T) {
	// The lsp tag section is appended to the ingest prompt when the
	// gopls-backed handler resolves in the scope. See
	// gotools.TheoryOfGopls and blocks.TheoryOfIngestBlocks.
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Call(func(
		prompt SystemPrompt,
	) {
		if !strings.Contains(string(prompt), "LSP Tag (Go sessions, gopls)") {
			t.Fatal("system prompt must include the lsp tag section")
		}
	})
}

func TestIngestComponentPassesLSPHandler(t *testing.T) {
	var gotQuery blocks.LSPQuery
	fake := blocks.LSPHandler(func(ctx context.Context, q blocks.LSPQuery) (string, error) {
		gotQuery = q
		return "fake lsp result", nil
	})
	scope := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() codetypes.PartsProvider { return mockPartsProvider{} },
	).Fork(
		func() blocks.LSPHandler { return fake },
	)
	scope.Call(func(comps CodesComponents) {
		root, err := os.OpenRoot(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		remaining, newState, _, _, _, triggered, err := components.ProcessComponents(
			context.Background(),
			comps.ComponentSet,
			[]blocks.Block{{Kind: "ingest", Body: `<lsp method="hover" symbol="Foo" />`}},
			generators.NewPrompts("", nil),
			root,
			nets.HTTPClient{&http.Client{}},
			nil,
		)
		if err != nil {
			t.Fatal(err)
		}
		if !triggered {
			t.Fatal("expected the ingest component to trigger a new round")
		}
		if len(remaining) != 0 {
			t.Fatalf("expected no remaining blocks, got %d", len(remaining))
		}
		if gotQuery.Method != "hover" || gotQuery.Symbol != "Foo" {
			t.Fatalf("unexpected query passed to the handler: %+v", gotQuery)
		}
		if newState == nil {
			t.Fatal("expected a modified state")
		}
		found := false
		for c := range newState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(generators.Text); ok && strings.Contains(string(text), "fake lsp result") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("expected the handler result in the new state")
		}
	})
}
