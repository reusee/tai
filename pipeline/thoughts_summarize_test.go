package pipeline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/reusee/tai/generators"
)

type mockSummarizerGenerator struct {
	summary     string
	err         error
	calls       int
	lastInput   string
	noHeader    bool
	prefixBlock bool
}

func (m *mockSummarizerGenerator) Spec() generators.Spec {
	return generators.Spec{}
}

func (m *mockSummarizerGenerator) CountTokens(text string) (int, error) {
	return len(text), nil
}

func (m *mockSummarizerGenerator) Generate(ctx context.Context, state generators.State, options *generators.GenerateOptions) (generators.State, error) {
	m.calls++
	if m.err != nil {
		return state, m.err
	}
	m.lastInput = ""
	for content := range state.Contents() {
		if content.Role == generators.RoleUser {
			for _, part := range content.Parts {
				if text, ok := part.(generators.Text); ok {
					m.lastInput += string(text)
				}
			}
		}
	}
	var blockOutput string
	switch {
	case m.prefixBlock:
		blockOutput = "<<龘靐 continue\ncontinue\n龘靐\n<<齉爩 summary\n" + m.summary + "\n齉爩"
	case m.noHeader:
		blockOutput = "<<龘靐\n" + m.summary + "\n龘靐"
	default:
		blockOutput = "<<龘靐 summary\n" + m.summary + "\n龘靐"
	}
	return state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Text(blockOutput),
		},
	})
}

func TestThoughtsSummarizePeriodicSummarization(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary of thoughts"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 50*time.Millisecond)

	// Append a thought — no summarization yet because interval hasn't elapsed
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thinking about something\n\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls before interval, got %d", gen.calls)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer before interval, got %q", buf.String())
	}

	// Wait for interval to elapse
	time.Sleep(60 * time.Millisecond)

	// Append another thought, which should trigger summarization
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("more thinking")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after interval, got %d", gen.calls)
	}
	if !strings.Contains(buf.String(), "summary of thoughts") {
		t.Fatalf("expected summary in buffer, got %q", buf.String())
	}
}

func TestThoughtsSummarizeFlushRemaining(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "flush summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	// Long interval so summarization only happens on flush
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thought 1"), generators.Thought("thought 2")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls before flush, got %d", gen.calls)
	}

	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call on flush, got %d", gen.calls)
	}
	if !strings.Contains(buf.String(), "flush summary") {
		t.Fatalf("expected flush summary in buffer, got %q", buf.String())
	}
}

func TestThoughtsSummarizeNonThoughtPassThrough(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("hello")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var contents []*generators.Content
	for c := range state.Contents() {
		contents = append(contents, c)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content in upstream, got %d", len(contents))
	}
	if text, ok := contents[0].Parts[0].(generators.Text); !ok || text != "hello" {
		t.Fatalf("unexpected content: %v", contents[0].Parts)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls for non-thought content, got %d", gen.calls)
	}
}

func TestThoughtsSummarizeNoSummarizeWhenEmpty(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	// Append non-thought content after interval — should not summarize
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("just text")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls when no thoughts, got %d", gen.calls)
	}

	// Flush with no thoughts should not summarize
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls on flush with no thoughts, got %d", gen.calls)
	}
}

func TestThoughtsSummarizeDefaultInterval(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	if state.interval != 3*time.Second {
		t.Fatalf("expected default interval of 3s, got %v", state.interval)
	}
}

func TestThoughtsSummarizeAccumulatesAcrossContents(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "combined summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thought A")},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thought B")},
	})
	if err != nil {
		t.Fatal(err)
	}

	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call on flush, got %d", gen.calls)
	}
	if !strings.Contains(buf.String(), "combined summary") {
		t.Fatalf("expected combined summary, got %q", buf.String())
	}
}

func TestThoughtsSummarizeSummarizeError(t *testing.T) {
	gen := &mockSummarizerGenerator{err: errors.New("summarize failed")}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thinking\n\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Next append should trigger summarization and return error
	_, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("more thinking")},
	})
	if err == nil {
		t.Fatal("expected error from summarization")
	}
	if !strings.Contains(err.Error(), "summarize failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSummarizerExtractsText(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "concise summary"}
	summarizer := NewSummarizer(gen)

	result, err := summarizer.Summarize(context.Background(), "some thoughts to summarize")
	if err != nil {
		t.Fatal(err)
	}
	if result != "concise summary" {
		t.Fatalf("expected 'concise summary', got %q", result)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call, got %d", gen.calls)
	}
}

func TestSummarizerFallsBackToKindlessBlock(t *testing.T) {
	// The model emits a summary block without the XML opening tag.
	// Such a block has no kind and can only be found by iterating all
	// blocks, so Summarize must fall back to the first block.
	// See TheoryOfKindlessBlocks.
	gen := &mockSummarizerGenerator{summary: "kindless summary", noHeader: true}
	summarizer := NewSummarizer(gen)

	result, err := summarizer.Summarize(context.Background(), "some thoughts")
	if err != nil {
		t.Fatal(err)
	}
	if result != "kindless summary" {
		t.Fatalf("expected 'kindless summary', got %q", result)
	}
}

func TestThoughtsSummarizeResetsAfterSummarization(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// First thought
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("first thought\n\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for interval, then trigger summarization with a non-thought
	// content. The flush-on-non-thought summarizes "first thought"
	// (accumulated from before) and resets accumulated to empty.
	time.Sleep(5 * time.Millisecond)
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after first summarization, got %d", gen.calls)
	}

	// Append a new thought after the reset. This thought was not included
	// in the first summarization because accumulated was cleared.
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("second thought")},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flush should summarize only "second thought" (the new thought
	// accumulated after the flush-on-non-thought reset).
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 calls after flush, got %d", gen.calls)
	}
}

func TestThoughtsSummarizeSplitsAtParagraphBoundary(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// Append a complete paragraph followed by an incomplete sentence
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("complete paragraph\n\nincomplete sent")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Non-thought content triggers a full flush of all accumulated
	// thoughts, regardless of paragraph boundaries, because the thought
	// stream has ended.
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call, got %d", gen.calls)
	}
	// All accumulated text is summarized when non-thought content arrives
	if gen.lastInput != "complete paragraph\n\nincomplete sent" {
		t.Fatalf("expected all accumulated text, got %q", gen.lastInput)
	}

	// Flush with no remaining thoughts should not summarize again
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after flush (no remaining), got %d", gen.calls)
	}
}

func TestThoughtsSummarizeDefersWithoutParagraphBoundary(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// Append a thought without any paragraph boundary
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("no paragraph boundary here")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Non-thought content triggers a full flush of accumulated thoughts,
	// regardless of paragraph boundaries, because the thought stream
	// has ended.
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleUser,
		Parts: []generators.Part{generators.Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call (flushed on non-thought), got %d", gen.calls)
	}

	// Flush with no remaining thoughts should not summarize again
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after flush (no remaining), got %d", gen.calls)
	}
}

func TestSplitAtLastCompleteParagraph(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		complete  string
		remaining string
	}{
		{"empty", "", "", ""},
		{"no boundary", "hello world", "", "hello world"},
		{"single newline", "hello\nworld", "hello\n", "world"},
		{"trailing newline", "hello\n", "hello\n", ""},
		{"double newline", "hello\n\nworld", "hello\n\n", "world"},
		{"trailing double newline", "hello\n\n", "hello\n\n", ""},
		{"multiple paragraphs", "para1\n\npara2\n\npara3", "para1\n\npara2\n\n", "para3"},
		{"only newlines", "\n\n", "\n\n", ""},
		{"incomplete after boundary", "done\n\nunfinished", "done\n\n", "unfinished"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complete, remaining := splitAtLastCompleteParagraph(tt.input)
			if complete != tt.complete {
				t.Errorf("complete: got %q, want %q", complete, tt.complete)
			}
			if remaining != tt.remaining {
				t.Errorf("remaining: got %q, want %q", remaining, tt.remaining)
			}
		})
	}
}

func TestSummarizeSystemPromptUsesUncommonChineseDelimiter(t *testing.T) {
	// The delimiter policy mandates an uncommon Chinese two-character word
	// per block. See TheoryOfBlockFormatGeneral in blocks/block.go.
	if !strings.Contains(SummarizeSystemPrompt, "uncommon Chinese two-character word") {
		t.Fatal("SummarizeSystemPrompt must mandate the uncommon-Chinese-two-character-word delimiter policy")
	}
	if strings.Contains(SummarizeSystemPrompt, "<<ENDSUM") {
		t.Fatal("SummarizeSystemPrompt must not display the legacy ENDSUM example delimiter")
	}
}

func TestSummarizerPrefersSummaryBlockOverEarlierBlocks(t *testing.T) {
	// When the model emits a non-summary block before the summary block,
	// Summarize must return the summary block's body, not the first
	// block's body.
	gen := &mockSummarizerGenerator{summary: "the real summary", prefixBlock: true}
	summarizer := NewSummarizer(gen)

	result, err := summarizer.Summarize(context.Background(), "some thoughts")
	if err != nil {
		t.Fatal(err)
	}
	if result != "the real summary" {
		t.Fatalf("expected 'the real summary', got %q", result)
	}
}

func TestThoughtsSummarizeUnwrap(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("system", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	u := state.Unwrap()
	if u == nil {
		t.Fatal("Unwrap returned nil")
	}
	if _, ok := u.(generators.Prompts); !ok {
		t.Fatalf("expected Unwrap to return Prompts, got %T", u)
	}
}

func TestThoughtsSummarizeSystemPromptDelegates(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("my system prompt", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	if state.SystemPrompt() != "my system prompt" {
		t.Fatalf("expected 'my system prompt', got %q", state.SystemPrompt())
	}
}

func TestThoughtsSummarizeFunctionsDelegate(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	fn := &generators.Function{Decl: generators.FuncDecl{Name: "test"}}
	upstream := generators.NewFuncMap(generators.NewPrompts("", nil), fn)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	var names []string
	for f := range state.Functions() {
		names = append(names, f.Decl.Name)
	}
	if len(names) != 1 || names[0] != "test" {
		t.Fatalf("expected function 'test', got %v", names)
	}
}

func TestThoughtsSummarizeFlushOnNonThought(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "flushed summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := generators.NewPrompts("", nil)
	// Long interval so periodic summarization doesn't trigger
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	// Accumulate thoughts
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("some thinking")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls before non-thought content, got %d", gen.calls)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty buffer, got %q", buf.String())
	}

	// Non-thought content triggers immediate flush of accumulated thoughts,
	// ensuring the summary appears before the main text output.
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Text("the answer")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after non-thought content, got %d", gen.calls)
	}
	if !strings.Contains(buf.String(), "flushed summary") {
		t.Fatalf("expected flushed summary in buffer, got %q", buf.String())
	}

	// Flush should not summarize again (accumulated was cleared)
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after flush, got %d", gen.calls)
	}
}

func TestThoughtsSummarizeFlushOnMixedContent(t *testing.T) {
	// When a single Content contains both Thought and Text parts (e.g., a
	// streaming chunk where the model transitions from reasoning to
	// answering), the summary must be flushed BEFORE the text is propagated
	// to upstream and printed.
	gen := &mockSummarizerGenerator{summary: "mixed summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	// Use NewOutput as upstream so text is also written to buf,
	// allowing us to verify the ordering of summary and text.
	upstream := generators.NewOutput(generators.NewPrompts("", nil), buf, false)
	// Long interval so periodic summarization doesn't interfere.
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	// Accumulate some thoughts first.
	state, err := state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("prior thinking")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls before mixed content, got %d", gen.calls)
	}

	// Mixed content: Thought + Text in the same Content.
	state, err = state.AppendContent(&generators.Content{
		Role: generators.RoleModel,
		Parts: []generators.Part{
			generators.Thought("final thought"),
			generators.Text("the answer"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// The summary should have been flushed before the text was propagated.
	if gen.calls != 1 {
		t.Fatalf("expected 1 call for mixed content flush, got %d", gen.calls)
	}

	// The summary input should include both the prior thinking and the
	// final thought from the mixed content.
	if gen.lastInput != "prior thinkingfinal thought" {
		t.Fatalf("expected 'prior thinkingfinal thought', got %q", gen.lastInput)
	}

	// The summary should appear in the buffer before the text.
	output := buf.String()
	summaryIdx := strings.Index(output, "mixed summary")
	answerIdx := strings.Index(output, "the answer")
	if summaryIdx == -1 {
		t.Fatal("summary not found in output")
	}
	if answerIdx == -1 {
		t.Fatal("answer not found in output")
	}
	if summaryIdx > answerIdx {
		t.Fatalf("summary should appear before answer, got summary at %d, answer at %d", summaryIdx, answerIdx)
	}
}

func TestThoughtsSummarizeNilSummarizer(t *testing.T) {
	buf := new(bytes.Buffer)
	upstream := generators.NewPrompts("", nil)
	// Create with nil summarizer
	var state generators.State = NewThoughtsSummarize(context.Background(), upstream, nil, buf)

	// Append content should pass through without error
	var err error
	state, err = state.AppendContent(&generators.Content{
		Role:  generators.RoleModel,
		Parts: []generators.Part{generators.Thought("thinking"), generators.Text("answer")},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flush should pass through
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}

	// Contents should be accessible
	var contents []*generators.Content
	for c := range state.Contents() {
		contents = append(contents, c)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
}
