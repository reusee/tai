package blocks

import (
	"errors"
	"testing"

	"github.com/reusee/tai/generators"
)

// TestParserStateFlushHandlerErrorPreservesPartialState verifies that a
// handler error at Flush returns the partial state rather than nil. A
// complete block behind a malformed one is first handled at Flush; if
// Flush discarded the state there, the generation loop's error-retry
// gate would see no content increase and stop the loop without feeding
// the error back to the model. See TheoryOfParserState.
func TestParserStateFlushHandlerErrorPreservesPartialState(t *testing.T) {
	upstream := generators.NewPrompts("system prompt", nil)
	before := generators.CountContents(upstream)

	handlerErr := errors.New("apply failed")
	var state generators.State = NewParserState(upstream, func(block Block) error {
		if block.Kind == "change" {
			return handlerErr
		}
		return nil
	})

	// The unclosed block at the head stops AppendContent's parser, so the
	// complete change block behind it survives to Flush unhandled.
	var err error
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("<<阑珊 notes\nnever closed\n<<翡翠 change(op=\"WRITE\", file-path=\"a.txt\")\nbody\n翡翠\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	flushed, err := state.(*ParserState).Flush()
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got %v", err)
	}
	if flushed == nil {
		t.Fatal("Flush returned a nil state on handler error; the partial output is lost and the retry gate cannot fire")
	}
	if got := generators.CountContents(flushed); got <= before {
		t.Fatalf("Flush dropped the partial output: content count %d, want > %d", got, before)
	}
	if parseErrors := flushed.(*ParserState).ParseErrors(); len(parseErrors) != 1 {
		t.Fatalf("expected the unclosed block to be collected as a parse error, got %d", len(parseErrors))
	}
}
