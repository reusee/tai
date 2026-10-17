package generators

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
)

// newTestJev builds a Jev generator for tests: the given HTTP client, a
// discarding logger, no event recorder, and the given function
// declarations as the request's questions.
func newTestJev(spec Spec, client *http.Client, decls FuncDecls) Jev {
	ret := Jev{
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

func TestJevGenerate(t *testing.T) {
	var got SystemOneRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected authorization %q", r.Header.Get("Authorization"))
		}
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
		if err := json.NewEncoder(w).Encode(SystemOneResponse{
			Model: "jev-1.13.0",
			Answers: map[string]SystemOneAnswer{
				"lookup": {
					Type: "noul",
					Noul: new(0.9),
				},
			},
			Usage: &SystemOneUsage{
				InputTokens:  12,
				OutputTokens: 3,
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	spec := Spec{
		Name:    "jev",
		Model:   "jev-latest",
		BaseURL: server.URL + "/v1",
	}
	jev := newTestJev(spec, server.Client(), FuncDecls{
		{
			Name:        "lookup",
			Description: "look up a record",
		},
	})

	state := NewPrompts("system prompt", []*Content{
		{
			Role:  RoleUser,
			Parts: []Part{Text("hello")},
		},
	})
	ret, err := jev.Generate(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// the request carried the model, the state, and one noul question per function
	if got.Model != "jev-latest" {
		t.Errorf("unexpected model %q", got.Model)
	}
	if got.State.System != "system prompt" {
		t.Errorf("unexpected system %q", got.State.System)
	}
	if len(got.State.Messages) != 1 || got.State.Messages[0].Text != "hello" {
		t.Errorf("unexpected messages %#v", got.State.Messages)
	}
	question, ok := got.Questions["lookup"]
	if !ok {
		t.Fatalf("expected a question for lookup, got %#v", got.Questions)
	}
	if question.Type != "noul" {
		t.Errorf("unexpected question type %q", question.Type)
	}

	// the answer selected the function
	var calls []FuncCall
	for content := range ret.Contents() {
		for _, part := range content.Parts {
			if call, ok := part.(FuncCall); ok {
				calls = append(calls, call)
			}
		}
	}
	if len(calls) != 1 {
		t.Fatalf("expected one function call, got %#v", calls)
	}
	if calls[0].Name != "lookup" || calls[0].ID != "lookup" {
		t.Errorf("unexpected call %#v", calls[0])
	}
}

func TestJevGenerateBelowThreshold(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(SystemOneResponse{
			Model: "jev-1.13.0",
			Answers: map[string]SystemOneAnswer{
				"lookup": {
					Type: "noul",
					Noul: new(0.4),
				},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	spec := Spec{
		Model:   "jev-latest",
		BaseURL: server.URL + "/v1",
	}
	jev := newTestJev(spec, server.Client(), FuncDecls{
		{Name: "lookup"},
	})

	state := NewPrompts("", []*Content{
		{
			Role:  RoleUser,
			Parts: []Part{Text("hello")},
		},
	})
	ret, err := jev.Generate(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for content := range ret.Contents() {
		for _, part := range content.Parts {
			if call, ok := part.(FuncCall); ok {
				t.Fatalf("unexpected function call %#v", call)
			}
		}
	}
}

func TestJevGenerateNoQuestions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s", r.URL)
	}))
	defer server.Close()

	spec := Spec{
		Model:        "jev-latest",
		BaseURL:      server.URL + "/v1",
		DisableTools: new(true),
	}
	jev := newTestJev(spec, server.Client(), nil)

	state := NewPrompts("", []*Content{
		{
			Role:  RoleUser,
			Parts: []Part{Text("hello")},
		},
	})
	if _, err := jev.Generate(context.Background(), state, nil); err == nil {
		t.Fatal("expected an error for a request without questions")
	}
}

func TestJevSelectedFunctions(t *testing.T) {
	answers := map[string]SystemOneAnswer{
		"a": {Type: "noul", Noul: new(0.9)},
		"b": {Type: "noul", Noul: new(0.4)},
		"c": {Type: "noul", Noul: new(0.5)},
		"d": {Type: "choice", Choice: "x"},
		"e": {Type: "noul"},
	}
	got := jevSelectedFunctions(answers)
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

// TestGetGeneratorJev covers the jev dispatch: the type "jev" routes to the
// Jev generator. It also guards the provider graph: building the generator
// resolves the TypeSafe API key from the scope, so a missing key provider
// fails here before any request is made. See TheoryOfJev.
func TestGetGeneratorJev(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.cue")
	configContent := `generators: [
  {
    name: "myjev"
    type: "jev"
    model: "jev-latest"
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

	generator, err := getGenerator("myjev")
	if err != nil {
		t.Fatal(err)
	}
	jev, ok := generator.(Jev)
	if !ok {
		t.Fatalf("expected a Jev generator, got %T", generator)
	}
	spec := jev.Spec()
	if spec.BaseURL != "https://api.typesafe.ai/v1" {
		t.Errorf("unexpected base url %q", spec.BaseURL)
	}
	if spec.Model != "jev-latest" {
		t.Errorf("unexpected model %q", spec.Model)
	}
}
