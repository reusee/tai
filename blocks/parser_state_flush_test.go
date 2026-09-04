package blocks

import (
	"testing"

	"github.com/reusee/tai/generators"
)

func TestParserStateFlushHandlerErrorPreservesPartialState(t *testing.T) {
	// A complete block preceded by a malformed opening is unreachable
	// during AppendContent: the parser stops at the malformed header, so
	// the handler is not called during streaming. Flush collects the
	// parse error, then calls the handler for the complete block; a
	// handler error must preserve the partial state: upstream contents
	// kept, buffer advanced past the failing block, and the state never
	// nil. See TheoryOfParserState.
	upstream := &mockState{systemPrompt: "system prompt"}
	expectedErr := &testHandlerError{msg: "handler failed"}
	ps := NewParserState(upstream, func(block Block) error {
		return expectedErr
	})

	text := "<<龘靐 change:?op=MODIFY&target\n<<齉爩 summary\nDone.\n齉爩\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if pending := ps.PendingText(); pending == "" {
		t.Fatal("expected pending text before flush: the parser must stop at the malformed block")
	}

	flushedState, err := ps.Flush()
	if err == nil {
		t.Fatal("expected handler error from Flush")
	}
	if err != expectedErr {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
	newPS, ok := flushedState.(*ParserState)
	if !ok {
		t.Fatalf("expected *ParserState, got %T", flushedState)
	}
	if pending := newPS.PendingText(); pending != "" {
		t.Fatalf("buffer should be advanced past the failing block, got %q", pending)
	}
}
