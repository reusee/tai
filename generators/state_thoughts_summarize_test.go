package generators

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockSummarizerGenerator struct {
	summary   string
	err       error
	calls     int
	lastInput string
}

func (m *mockSummarizerGenerator) Spec() Spec {
	return Spec{}
}

func (m *mockSummarizerGenerator) CountTokens(text string) (int, error) {
	return len(text), nil
}

func (m *mockSummarizerGenerator) Generate(ctx context.Context, state State, options *GenerateOptions) (State, error) {
	m.calls++
	if m.err != nil {
		return state, m.err
	}
	// Capture the input text from user content for test assertions
	m.lastInput = ""
	for content := range state.Contents() {
		if content.Role == RoleUser {
			for _, part := range content.Parts {
				if text, ok := part.(Text); ok {
					m.lastInput += string(text)
				}
			}
		}
	}
	return state.AppendContent(&Content{
		Role: RoleModel,
		Parts: []Part{
			Text(m.summary),
		},
	})
}

func TestThoughtsSummarizePeriodicSummarization(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary of thoughts"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 50*time.Millisecond)

	// Append a thought — no summarization yet because interval hasn't elapsed
	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("thinking about something\n\n")},
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
	state, err = state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("more thinking")},
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

	upstream := NewPrompts("", nil)
	// Long interval so summarization only happens on flush
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("thought 1"), Thought("thought 2")},
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

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("hello")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var contents []*Content
	for c := range state.Contents() {
		contents = append(contents, c)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content in upstream, got %d", len(contents))
	}
	if text, ok := contents[0].Parts[0].(Text); !ok || text != "hello" {
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

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	time.Sleep(5 * time.Millisecond)

	// Append non-thought content after interval — should not summarize
	state, err := state.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("just text")},
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

	upstream := NewPrompts("", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	if state.interval != 3*time.Second {
		t.Fatalf("expected default interval of 3s, got %v", state.interval)
	}
}

func TestThoughtsSummarizeAccumulatesAcrossContents(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "combined summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 10*time.Second)

	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("thought A")},
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err = state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("thought B")},
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

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("thinking\n\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Next append should trigger summarization and return error
	_, err = state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("more thinking")},
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

func TestThoughtsSummarizeResetsAfterSummarization(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// First thought
	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("first thought\n\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for interval, then trigger summarization with a non-thought
	// content. The periodic check summarizes "first thought" (accumulated
	// from before) and resets accumulated to empty.
	time.Sleep(5 * time.Millisecond)
	state, err = state.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call after first summarization, got %d", gen.calls)
	}

	// Append a new thought after the reset. This thought was not included
	// in the first summarization because accumulated was cleared.
	state, err = state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("second thought")},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flush should summarize only "second thought" (the new thought
	// accumulated after the periodic summarization reset).
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

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// Append a complete paragraph followed by an incomplete sentence
	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("complete paragraph\n\nincomplete sent")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Trigger summarization with a non-thought content
	state, err = state.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call, got %d", gen.calls)
	}
	// Only the complete paragraph should have been summarized
	if gen.lastInput != "complete paragraph\n\n" {
		t.Fatalf("expected only complete paragraph, got %q", gen.lastInput)
	}

	// The remaining "incomplete sent" should be summarized on flush
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("expected 2 calls after flush, got %d", gen.calls)
	}
	if gen.lastInput != "incomplete sent" {
		t.Fatalf("expected remaining text on flush, got %q", gen.lastInput)
	}
}

func TestThoughtsSummarizeDefersWithoutParagraphBoundary(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("", nil)
	var state State = NewThoughtsSummarize(context.Background(), upstream, summarizer, buf, 1*time.Millisecond)

	// Append a thought without any paragraph boundary
	state, err := state.AppendContent(&Content{
		Role:  RoleModel,
		Parts: []Part{Thought("no paragraph boundary here")},
	})
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	// Trigger summarization attempt with a non-thought content
	state, err = state.AppendContent(&Content{
		Role:  RoleUser,
		Parts: []Part{Text("trigger")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Summarization should be deferred because there's no paragraph boundary
	if gen.calls != 0 {
		t.Fatalf("expected 0 calls (deferred), got %d", gen.calls)
	}

	// Flush should summarize all remaining text regardless of boundaries
	state, err = state.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if gen.calls != 1 {
		t.Fatalf("expected 1 call on flush, got %d", gen.calls)
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

func TestThoughtsSummarizeUnwrap(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("system", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	u := state.Unwrap()
	if u == nil {
		t.Fatal("Unwrap returned nil")
	}
	if _, ok := u.(Prompts); !ok {
		t.Fatalf("expected Unwrap to return Prompts, got %T", u)
	}
}

func TestThoughtsSummarizeSystemPromptDelegates(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	upstream := NewPrompts("my system prompt", nil)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	if state.SystemPrompt() != "my system prompt" {
		t.Fatalf("expected 'my system prompt', got %q", state.SystemPrompt())
	}
}

func TestThoughtsSummarizeFunctionsDelegate(t *testing.T) {
	gen := &mockSummarizerGenerator{summary: "summary"}
	summarizer := NewSummarizer(gen)
	buf := new(bytes.Buffer)

	fn := &Function{Decl: FuncDecl{Name: "test"}}
	upstream := WithFunctions(NewPrompts("", nil), fn)
	state := NewThoughtsSummarize(context.Background(), upstream, summarizer, buf)

	var names []string
	for f := range state.Functions() {
		names = append(names, f.Decl.Name)
	}
	if len(names) != 1 || names[0] != "test" {
		t.Fatalf("expected function 'test', got %v", names)
	}
}
