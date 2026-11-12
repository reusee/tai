package generators

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
)

// newTestDecisions builds a Decisions generator for tests: the given HTTP
// client, a discarding logger, no event recorder, and the given function
// declarations as the request's questions.
func newTestDecisions(spec Spec, client *http.Client, decls FuncDecls) Decisions {
	ret := Decisions{
		spec:   spec,
		apiKey: "test-key",
		client: nets.HTTPClient{Client: client},
	}
	ret.Count = func() BPETokenCounter {
		return BPETokenCounter(func(text string) (int, error) {
			return len(text), nil
		})
	}
	ret.Logger = func() logs.Logger {
		return logs.Logger{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
	}
	ret.EventRecorder = func() EventRecorder {
		return nil
	}
	ret.FuncDecls = func() FuncDecls {
		return decls
	}
	return ret
}

func TestDecisionsGenerate(t *testing.T) {
	var got DecisionsRequest
	var gotKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/decisions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotKey = r.Header.Get("x-goog-api-key")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(DecisionsResponse{
			Answers: map[string]DecisionsAnswer{
				"lookup": {Type: "noul", Noul: new(0.93), Confidence: new(0.93)},
				"store":  {Type: "noul", Noul: new(0.12), Confidence: new(0.88)},
			},
			Usage: &DecisionsUsage{
				TotalInputTokens: 48,
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	spec := Spec{
		Name:    "decisions",
		Model:   "gemini-decision-1",
		BaseURL: server.URL + "/v1beta",
	}
	gen := newTestDecisions(spec, server.Client(), FuncDecls{
		{Name: "lookup", Description: "look up a record"},
		{Name: "store"},
	})

	state := NewPrompts("system prompt", []*Content{
		{
			Role: RoleUser,
			// the two text fragments concatenate verbatim into one
			// block, and the block ends with a blank line
			Parts: []Part{Text("he"), Text("llo")},
		},
	})
	ret, err := gen.Generate(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if gotKey != "test-key" {
		t.Errorf("unexpected api key header %q", gotKey)
	}
	if got.Model != "gemini-decision-1" {
		t.Errorf("unexpected model %q", got.Model)
	}
	// the system prompt opens the input, ahead of the conversation
	if len(got.Input) < 2 {
		t.Fatalf("unexpected input %#v", got.Input)
	}
	if got.Input[0].Type != "text" || got.Input[0].Text != "system prompt\n\n" {
		t.Errorf("unexpected first input block %#v", got.Input[0])
	}
	if got.Input[1].Type != "text" || got.Input[1].Text != "hello\n\n" {
		t.Errorf("unexpected second input block %#v", got.Input[1])
	}
	question, ok := got.Questions["lookup"]
	if !ok {
		t.Fatalf("expected a question for lookup, got %#v", got.Questions)
	}
	if question.Type != "noul" {
		t.Errorf("unexpected question type %q", question.Type)
	}
	if !strings.Contains(question.Instructions, "lookup") {
		t.Errorf("expected the function name in the instructions, got %q", question.Instructions)
	}

	// only the answer above the threshold selects its function, and the
	// full answer map reaches the log as the evidence
	var calls []FuncCall
	var answersLog string
	for content := range ret.Contents() {
		for _, part := range content.Parts {
			switch part := part.(type) {
			case FuncCall:
				calls = append(calls, part)
			case Text:
				if content.Role == RoleLog && strings.Contains(string(part), "decisions answers") {
					answersLog = string(part)
				}
			case Usage:
				if part.Prompt.TokenCount != 48 {
					t.Errorf("unexpected prompt tokens %d", part.Prompt.TokenCount)
				}
			}
		}
	}
	if len(calls) != 1 {
		t.Fatalf("expected one function call, got %#v", calls)
	}
	if calls[0].Name != "lookup" || calls[0].ID != "lookup" {
		t.Errorf("unexpected call %#v", calls[0])
	}
	if !strings.Contains(answersLog, "0.93") {
		t.Errorf("expected the answers in the log, got %q", answersLog)
	}
}

func TestDecisionsGenerateNoQuestions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
	}))
	defer server.Close()

	gen := newTestDecisions(Spec{
		Model:        "gemini-decision-1",
		BaseURL:      server.URL,
		DisableTools: new(true),
	}, server.Client(), nil)

	state := NewPrompts("", []*Content{
		{
			Role:  RoleUser,
			Parts: []Part{Text("hello")},
		},
	})
	if _, err := gen.Generate(context.Background(), state, nil); err == nil {
		t.Fatal("expected an error for a request without questions")
	}
}

// TestDecisionsGenerateMedia covers the media input path: a file part becomes
// a typed media block carrying base64 bytes, and the text runs around it stay
// separate blocks in conversation order. See TheoryOfDecisions.
func TestDecisionsGenerateMedia(t *testing.T) {
	var got DecisionsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(DecisionsResponse{
			Answers: map[string]DecisionsAnswer{
				"describe": {Type: "noul", Noul: new(0.8)},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	gen := newTestDecisions(Spec{
		Model:   "gemini-decision-1",
		BaseURL: server.URL,
	}, server.Client(), FuncDecls{
		{Name: "describe"},
	})

	image := []byte("fake-image")
	state := NewPrompts("", []*Content{
		{
			Role: RoleUser,
			Parts: []Part{
				Text("what is this?"),
				FileContent{Content: image, MimeType: "image/png"},
				Text("answer briefly"),
			},
		},
	})
	if _, err := gen.Generate(context.Background(), state, nil); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if len(got.Input) != 3 {
		t.Fatalf("unexpected input blocks %#v", got.Input)
	}
	if got.Input[0].Type != "text" || got.Input[0].Text != "what is this?\n\n" {
		t.Errorf("unexpected first block %#v", got.Input[0])
	}
	if got.Input[1].Type != "image" || got.Input[1].MimeType != "image/png" {
		t.Errorf("unexpected media block %#v", got.Input[1])
	}
	if got.Input[1].Data != base64.StdEncoding.EncodeToString(image) {
		t.Errorf("unexpected media data %q", got.Input[1].Data)
	}
	if got.Input[2].Type != "text" || got.Input[2].Text != "answer briefly\n\n" {
		t.Errorf("unexpected trailing block %#v", got.Input[2])
	}
}

// TestDecisionsRetryableStatus covers the failure path: a server status joins
// ErrRetryable, and the API's own error object reaches the report.
// See TheoryOfDecisions.
func TestDecisionsRetryableStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := w.Write([]byte(`{"error":{"code":"rate_limit","message":"slow down"}}`)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer server.Close()

	gen := newTestDecisions(Spec{
		Model:   "gemini-decision-1",
		BaseURL: server.URL,
	}, server.Client(), FuncDecls{
		{Name: "lookup"},
	})

	state := NewPrompts("", []*Content{
		{
			Role:  RoleUser,
			Parts: []Part{Text("hello")},
		},
	})
	_, err := gen.Generate(context.Background(), state, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrRetryable) {
		t.Fatalf("expected a retryable error, got %v", err)
	}
	if !strings.Contains(err.Error(), "rate_limit") || !strings.Contains(err.Error(), "slow down") {
		t.Fatalf("expected the API error object in the report, got %v", err)
	}
}

func TestDecisionsQuestionKey(t *testing.T) {
	taken := make(map[string]bool)
	got := decisionsQuestionKey("repo.read_file", taken)
	if got != "repo_read_file" {
		t.Errorf("unexpected key %q", got)
	}
	taken[got] = true
	dup := decisionsQuestionKey("repo/read_file", taken)
	if dup == got {
		t.Fatalf("expected a distinct key for a colliding name, got %q", dup)
	}
	long := decisionsQuestionKey(strings.Repeat("a", 100), taken)
	if len(long) > decisionsMaxQuestionKey {
		t.Errorf("expected a capped key, got %d characters", len(long))
	}
}

func TestDecisionsSelectedFunctions(t *testing.T) {
	answers := map[string]DecisionsAnswer{
		"a": {Type: "noul", Noul: new(0.9)},
		"b": {Type: "noul", Noul: new(0.4)},
		"c": {Type: "noul", Noul: new(0.5)},
		"d": {Type: "choice", Choice: "x"},
		"e": {Type: "noul"},
	}
	got := decisionsSelectedFunctions(answers)
	expected := []string{"a", "c"}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}

// TestGetGeneratorDecisions covers the decisions dispatch: the type
// "decisions" routes to the Decisions generator with the documented default
// endpoint and model. See TheoryOfDecisions.
func TestGetGeneratorDecisions(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.cue")
	configContent := `generators: [
  {
    name: "mydecisions"
    type: "decisions"
  },
]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	getGenerator := dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() configs.Loader {
			return configs.NewLoader([]string{configPath}, configs.LoaderConfig{})
		},
	).Get[GetGenerator]()

	generator, err := getGenerator("mydecisions")
	if err != nil {
		t.Fatal(err)
	}
	decisions, ok := generator.(Decisions)
	if !ok {
		t.Fatalf("expected a Decisions generator, got %T", generator)
	}
	spec := decisions.Spec()
	if spec.BaseURL != decisionsBaseURL {
		t.Errorf("unexpected base url %q", spec.BaseURL)
	}
	if spec.Model != decisionsModel {
		t.Errorf("unexpected model %q", spec.Model)
	}
}

// TestDecisionsErrorUnwrapsTransportError covers the wrapper's Unwrap: a
// transport fault wrapped in DecisionsError must still reach the retry
// classifier's identity checks. See TheoryOfTransientTransportErrors.
func TestDecisionsErrorUnwrapsTransportError(t *testing.T) {
	broken := DecisionsError{Err: io.ErrUnexpectedEOF}
	if !isRetryable(broken) {
		t.Fatalf("a broken transport must be retryable through the wrapper: %v", broken)
	}

	cancelled := DecisionsError{Err: context.Canceled}
	if isRetryable(cancelled) {
		t.Fatalf("a cancelled request must not be retried: %v", cancelled)
	}
}
