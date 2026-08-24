package blocks

import (
	"iter"
	"testing"

	"github.com/reusee/tai/generators"
)

// mockState is a minimal State implementation for testing ParserState.
type mockState struct {
	systemPrompt string
	contents     []*generators.Content
}

func (m *mockState) Contents() iter.Seq[*generators.Content] {
	return func(yield func(*generators.Content) bool) {
		for _, c := range m.contents {
			if !yield(c) {
				return
			}
		}
	}
}

func (m *mockState) AppendContent(content *generators.Content) (generators.State, error) {
	m.contents = append(m.contents, content)
	return m, nil
}

func (m *mockState) SystemPrompt() string {
	return m.systemPrompt
}

func (m *mockState) Functions() iter.Seq[*generators.Function] {
	return func(yield func(*generators.Function) bool) {}
}

func (m *mockState) Flush() (generators.State, error) {
	return m, nil
}

func (m *mockState) Unwrap() generators.State {
	return nil
}

func TestParserStateStreamParsing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Fragment 1: prose only, no block marker
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("I'll fix the issue.\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before any block marker, got %d", len(collectedBlocks))
	}

	// Fragment 2: opening marker and partial body (no end marker yet)
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks for incomplete block, got %d", len(collectedBlocks))
	}

	// Fragment 3: end marker completes the block
	newState, err = ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("龘靐\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" {
		t.Fatalf("expected kind change, got %s", collectedBlocks[0].Kind)
	}
	if collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", collectedBlocks[0].Boundary)
	}
}

func TestParserStateLenientFullStopStreaming(t *testing.T) {
	var got []Block
	var state generators.State = NewParserState(
		generators.NewPrompts("", nil),
		func(block Block) error {
			got = append(got, block)
			return nil
		},
	)
	var err error
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("prose。<<龃龉 change(op=\"MODIFY\")\nbody"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("unexpected blocks before the closing marker: %+v", got)
	}
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text("\n龃龉\n"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("blocks = %+v", got)
	}
	if got[0].Kind != "change" || got[0].Body != "body" {
		t.Fatalf("block = %+v", got[0])
	}
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if errs := state.(*ParserState).ParseErrors(); len(errs) > 0 {
		t.Fatalf("parse errors = %v", errs)
	}
}

func TestParserStateMultipleBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n<<齉爩 change(op=\"DELETE\", target=\"Bar\", file-path=\"/test.go\")\n齉爩\n<<麤黿 summary\n- Done.\n麤黿\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("unexpected first block: %+v", collectedBlocks[0])
	}
	if collectedBlocks[1].Kind != "change" || collectedBlocks[1].Boundary != "齉爩" {
		t.Fatalf("unexpected second block: %+v", collectedBlocks[1])
	}
	if collectedBlocks[2].Kind != "summary" || collectedBlocks[2].Boundary != "麤黿" {
		t.Fatalf("unexpected third block: %+v", collectedBlocks[2])
	}
}

func TestParserStateBareKindBlock(t *testing.T) {
	// A block whose opening marker carries a bare kind — <<DELIMITER
	// kind — is parsed with that kind during streaming, matching the
	// XML opening tag form. See TheoryOfBareKinds.
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 summary\n- done\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "summary" {
		t.Fatalf("expected kind summary, got %s", collectedBlocks[0].Kind)
	}
	if collectedBlocks[0].Body != "- done" {
		t.Fatalf("expected body '- done', got %q", collectedBlocks[0].Body)
	}
}

func TestParserStateUnwrapAndPassthrough(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	state := NewParserState(upstream)

	if state.Unwrap() != upstream {
		t.Fatal("Unwrap should return the upstream state")
	}
	if state.SystemPrompt() != "system prompt" {
		t.Fatalf("SystemPrompt should be %q, got %q", "system prompt", state.SystemPrompt())
	}
}

func TestParserStateIgnoresUserRole(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// User role content should not be parsed for blocks
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 0 {
		t.Fatalf("user role content should not produce blocks, got %d", len(collectedBlocks))
	}
}

func TestParserStateIgnoresThoughts(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// A Thought part containing complete block markers must not produce
	// a block, because thoughts are model reasoning, not block output.
	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"),
		},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks from thought part, got %d", len(collectedBlocks))
	}
	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty buffer, got %q", pending)
	}

	// A Text part following a Thought part must still be parsed normally,
	// and the Thought's block markers must not combine with the Text.
	content2 := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{
			generators.Thought("<<齉爩 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nbody\n齉爩\n"),
			generators.Text("<<麤黿 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/test.go\")\nfunc Bar() {}\n麤黿\n"),
		},
	}
	newState, err = ps.AppendContent(content2)
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block from text part, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "麤黿" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
}

func TestParserStatePendingText(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	// Append incomplete block
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("prose before\n<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nbody")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	pending := ps.PendingText()
	if pending == "" {
		t.Fatal("PendingText should not be empty for incomplete block")
	}
	if !contains(pending, "<<龘靐") {
		t.Fatalf("PendingText should contain the opening marker: %q", pending)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestParserStateNonMatchingEndIsBodyContent(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	content := &generators.Content{
		Role: generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(
			"<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n齉爩\n",
		)},
	}
	newState, err := ps.AppendContent(content)
	if err != nil {
		t.Fatalf("expected no error for non-matching end marker treated as body content, got %v", err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks for unclosed block, got %d", len(collectedBlocks))
	}
	pending := ps.PendingText()
	if !contains(pending, "<<龘靐") {
		t.Fatalf("pending text should contain the opening marker: %q", pending)
	}
}

func TestParserStateNonMatchingEndInBodyThenMatchingEnd(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nbody line 1\n齉爩\nbody line 2\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "齉爩") {
		t.Fatalf("body should contain non-matching delimiter line as content: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "body line 1") || !contains(collectedBlocks[0].Body, "body line 2") {
		t.Fatalf("body should contain both body lines: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateNestedBlocksSameDelimiter(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Outer\", file-path=\"/outer.go\")\n<<龘靐 change(op=\"MODIFY\", target=\"Inner\", file-path=\"/inner.go\")\nfunc Inner() {}\n龘靐\nfunc Outer() {}\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block (outer), got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "func Inner() {}") {
		t.Fatalf("outer body should contain inner block body: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "func Outer() {}") {
		t.Fatalf("outer body should contain outer block body: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "<<龘靐") {
		t.Fatalf("outer body should contain inner block opening marker: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateNestedDifferentDelimiterOpeningWithoutClosing(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\n<<齉爩 tag\nfunc Foo() {}\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("expected boundary 龘靐, got %s", collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "<<齉爩 tag") {
		t.Fatalf("body should contain the different-delimiter opening as content: %q", collectedBlocks[0].Body)
	}
	if !contains(collectedBlocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", collectedBlocks[0].Body)
	}
}

func TestParserStateFlushCollectsParseErrors(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(collectedBlocks))
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "龘靐" {
		t.Fatalf("expected parse error kind=change boundary=龘靐, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateFlushCollectsTruncatedOpeningLineParseError(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text("<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")")},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)
	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(collectedBlocks))
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for truncated opening line, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "龘靐" {
		t.Fatalf("expected parse error kind=change boundary=龘靐, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateFlushSucceedsWithCompleteBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block before flush, got %d", len(collectedBlocks))
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush should succeed with no unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after flush, got %q", pending)
	}
}

func TestParserStateFlushCollectsMultipleParseErrors(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n" +
		"<<齉爩 change(op=\"DELETE\", target=\"Bar\", file-path=\"/test.go\")\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 2 {
		t.Fatalf("expected 2 parse errors, got %d", len(parseErrors))
	}
	if parseErrors[0].Boundary != "龘靐" {
		t.Fatalf("expected first parse error boundary 龘靐, got %s", parseErrors[0].Boundary)
	}
	if parseErrors[1].Boundary != "齉爩" {
		t.Fatalf("expected second parse error boundary 齉爩, got %s", parseErrors[1].Boundary)
	}
}

func TestParserStateFlushHandlesCompleteBlocksAfterUnclosed(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n" +
		"<<齉爩 summary\nDone.\n齉爩\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 0 {
		t.Fatalf("expected 0 blocks before flush, got %d", len(collectedBlocks))
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	if len(ps.ParseErrors()) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(ps.ParseErrors()))
	}
	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 handled block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "summary" {
		t.Fatalf("expected summary block, got %s", collectedBlocks[0].Kind)
	}
}

func TestParserStateFlushCollectsParseErrorsAfterCompleteBlocks(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 summary\nDone.\n龘靐\n" +
		"<<齉爩 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 complete block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "summary" {
		t.Fatalf("expected summary block, got %s", collectedBlocks[0].Kind)
	}

	flushedState, err := ps.Flush()
	if err != nil {
		t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
	}
	ps = flushedState.(*ParserState)

	parseErrors := ps.ParseErrors()
	if len(parseErrors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
	}
	if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "齉爩" {
		t.Fatalf("expected parse error kind=change boundary=齉爩, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
	}
}

func TestParserStateEndMarkerNoTrailingNewline(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	var collectedBlocks []Block
	ps := NewParserState(upstream, func(block Block) error {
		collectedBlocks = append(collectedBlocks, block)
		return nil
	})

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if len(collectedBlocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(collectedBlocks))
	}
	if collectedBlocks[0].Kind != "change" || collectedBlocks[0].Boundary != "龘靐" {
		t.Fatalf("unexpected block: kind=%s boundary=%s", collectedBlocks[0].Kind, collectedBlocks[0].Boundary)
	}
	if !contains(collectedBlocks[0].Body, "func Foo() {}") {
		t.Fatalf("body should contain the code: %q", collectedBlocks[0].Body)
	}
	if contains(collectedBlocks[0].Body, "龘靐") {
		t.Fatalf("body should not contain the end marker: %q", collectedBlocks[0].Body)
	}

	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text, got %q", pending)
	}
}

type testHandlerError struct {
	msg string
}

func TestParserStateBlockHandler(t *testing.T) {
	t.Run("ReceivesBlocks", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var handledBlocks []Block
		ps := NewParserState(upstream, func(block Block) error {
			handledBlocks = append(handledBlocks, block)
			return nil
		})

		text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		if len(handledBlocks) != 1 {
			t.Fatalf("expected 1 handled block, got %d", len(handledBlocks))
		}
		if handledBlocks[0].Kind != "change" {
			t.Fatalf("expected change block, got %s", handledBlocks[0].Kind)
		}
	})

	t.Run("ErrorStopsStreaming", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		expectedErr := &testHandlerError{msg: "apply failed"}
		ps := NewParserState(upstream, func(block Block) error {
			if block.Kind == "change" {
				return expectedErr
			}
			return nil
		})

		text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"
		_, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err == nil {
			t.Fatal("expected error from handler")
		}
		if err != expectedErr {
			t.Fatalf("expected %v, got %v", expectedErr, err)
		}
	})

	t.Run("HandlerPropagatedToNewState", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var callCount int
		ps := NewParserState(upstream, func(block Block) error {
			callCount++
			return nil
		})

		text1 := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text1)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if callCount != 1 {
			t.Fatalf("expected 1 handler call, got %d", callCount)
		}

		text2 := "<<齉爩 change(op=\"MODIFY\", target=\"Bar\", file-path=\"/test.go\")\nfunc Bar() {}\n齉爩\n"
		newState, err = ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text2)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)
		if callCount != 2 {
			t.Fatalf("expected 2 handler calls, got %d", callCount)
		}
	})

	t.Run("HandlerNotCalledForUnclosedDuringFlush", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		var handledBlocks []Block
		ps := NewParserState(upstream, func(block Block) error {
			handledBlocks = append(handledBlocks, block)
			return nil
		})

		text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		if len(handledBlocks) != 0 {
			t.Fatalf("expected 0 handled blocks before flush, got %d", len(handledBlocks))
		}

		flushedState, err := ps.Flush()
		if err != nil {
			t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
		}
		ps = flushedState.(*ParserState)

		if len(handledBlocks) != 0 {
			t.Fatalf("expected 0 handled blocks after flush (unclosed block not applied), got %d", len(handledBlocks))
		}
		if len(ps.ParseErrors()) != 1 {
			t.Fatalf("expected 1 parse error, got %d", len(ps.ParseErrors()))
		}
	})

	t.Run("UnclosedBlockCollectedAsParseError", func(t *testing.T) {
		upstream := &mockState{systemPrompt: "system prompt"}
		ps := NewParserState(upstream, func(block Block) error {
			return nil
		})

		text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {\n\t// truncated"
		newState, err := ps.AppendContent(&generators.Content{
			Role:  generators.RoleAssistant,
			Parts: []generators.Part{generators.Text(text)},
		})
		if err != nil {
			t.Fatal(err)
		}
		ps = newState.(*ParserState)

		flushedState, err := ps.Flush()
		if err != nil {
			t.Fatalf("Flush must not return an error for unclosed blocks, got: %v", err)
		}
		ps = flushedState.(*ParserState)

		parseErrors := ps.ParseErrors()
		if len(parseErrors) != 1 {
			t.Fatalf("expected 1 parse error, got %d", len(parseErrors))
		}
		if parseErrors[0].BlockKind != "change" || parseErrors[0].Boundary != "龘靐" {
			t.Fatalf("expected parse error kind=change boundary=龘靐, got kind=%q boundary=%q", parseErrors[0].BlockKind, parseErrors[0].Boundary)
		}
	})
}

func TestParserStateNoHandler(t *testing.T) {
	upstream := &mockState{systemPrompt: "system prompt"}
	ps := NewParserState(upstream)

	text := "<<龘靐 change(op=\"MODIFY\", target=\"Foo\", file-path=\"/test.go\")\nfunc Foo() {}\n龘靐\n"
	newState, err := ps.AppendContent(&generators.Content{
		Role:  generators.RoleAssistant,
		Parts: []generators.Part{generators.Text(text)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ps = newState.(*ParserState)

	if pending := ps.PendingText(); pending != "" {
		t.Fatalf("expected empty pending text after block parsed without handler, got %q", pending)
	}
}

func (e *testHandlerError) Error() string {
	return e.msg
}
