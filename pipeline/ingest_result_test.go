package pipeline

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/tai/components"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/tree"
)

// TestRunIngestResultNodeCarriesContent reproduces the Tree tab bug:
// the block-result node under an ingest block node carried empty
// content, so expanding the node in the Tree tab showed nothing. The
// ingest component must deliver its fetch results through
// ProcessResult.Parts, so the component output's parts reach both the
// round's user content and the block-result node's content. The ingest
// block node's type is BlockType("ingest") — the block kind expressed as
// a structured type. See tree.TheoryOfTree.
func TestRunIngestResultNodeCarriesContent(t *testing.T) {
	withRun(t, func(run Run) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello-from-file"), 0o644); err != nil {
			t.Fatal(err)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()

		callCount := 0
		phaseBuilder := func(g generators.Generator) generators.Phase {
			callCount++
			if callCount == 1 {
				return appendPhase("<<萬曆 ingest\n<file path=\"a.txt\" />\n萬曆\n<<天祐 summary\nDone.\n天祐\n")
			}
			return appendPhase("<<天祐 summary\nSecond round done.\n天祐\n")
		}
		result, err := runOnce(run, RunOptions{
			Generator:    nil,
			InitialState: generators.NewPrompts("", nil),
			Components:   components.ComponentSet{NewIngestComponent(nil)},
			PhaseBuilder: phaseBuilder,
			Root:         root,
			HTTPClient:   nets.HTTPClient{&http.Client{}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 generations (the ingest fetch triggers the second), got %d", callCount)
		}
		if result.SessionTree == nil {
			t.Fatal("expected the result to carry the session tree")
		}
		var ingestNode *tree.Node
		for _, n := range result.SessionTree.ByType(tree.BlockType("ingest")) {
			if strings.Contains(n.Content, `file path="a.txt"`) {
				ingestNode = n
			}
		}
		if ingestNode == nil {
			t.Fatal("expected an ingest block node in the session tree")
		}
		kids := ingestNode.Children()
		if len(kids) != 1 || kids[0].Type != tree.TypeBlockResult {
			t.Fatalf("the ingest block node must carry a block-result child, got %+v", kids)
		}
		if strings.TrimSpace(kids[0].Content) == "" {
			t.Fatal("the block-result node must carry the fetched content, got empty content")
		}
		if !strings.Contains(kids[0].Content, "hello-from-file") {
			t.Fatalf("the block-result node must carry the fetched file content, got %q", kids[0].Content)
		}
	})
}
