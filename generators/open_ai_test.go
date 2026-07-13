package generators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/modes"
)

// errorAfterNState wraps a State and fails AppendContent after maxCalls
// successful calls. Used to test that Generate preserves partial state
// when AppendContent fails mid-stream.
type errorAfterNState struct {
	State
	calls    int
	maxCalls int
}

func (s *errorAfterNState) AppendContent(content *Content) (State, error) {
	s.calls++
	if s.calls > s.maxCalls {
		return s, errors.New("append content failed")
	}
	inner, err := s.State.AppendContent(content)
	if err != nil {
		return s, err
	}
	s.State = inner
	return s, nil
}

func TestOpenAI(t *testing.T) {
	testGenerator(t, func(
		newOpenRouter NewOpenRouter,
	) Generator {
		return newOpenRouter(Spec{
			Model:             "openai/gpt-oss-120b:free",
			ContextTokens:     128 << 10,
			MaxGenerateTokens: new(8 << 10),
		})
	})
}

func TestStateToOpenAIMessages(t *testing.T) {

	t.Run("merge model messages separated by log messages", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleLog,
				Parts: []Part{
					Usage{},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("foo"),
				},
			},
			{
				Role: RoleLog,
				Parts: []Part{
					Usage{},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("bar"),
				},
			},
		})

		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}

		if len(messages) != 1 {
			t.Fatalf("got %+v", messages)
		}
		if contentStr, ok := messages[0].Content.(string); !ok || contentStr != "foobar" {
			t.Fatalf("got %+v", messages)
		}

	})

	t.Run("log content with text is filtered", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleLog,
				Parts: []Part{
					Text("internal log message"),
				},
			},
			{
				Role: RoleUser,
				Parts: []Part{
					Text("user message"),
				},
			},
		})

		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}

		// The log content must be filtered out entirely; only the user
		// message should appear. Without filtering, the log message would
		// be sent to the API with an invalid role "log", corrupting the
		// request and destabilizing the prefix cache.
		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d: %+v", len(messages), messages)
		}
		if messages[0].Role != string(RoleUser) {
			t.Fatalf("expected user role, got %s", messages[0].Role)
		}
		if contentStr, ok := messages[0].Content.(string); !ok || contentStr != "user message" {
			t.Fatalf("expected 'user message', got %v", messages[0].Content)
		}
	})

	t.Run("reasoning content", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			},
		})
		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 {
			t.Fatalf("got %+v", messages)
		}
		if messages[0].ReasoningContent != "thinking" {
			t.Errorf("wrong reasoning: %s", messages[0].ReasoningContent)
		}
		if contentStr, ok := messages[0].Content.(string); !ok || contentStr != "answer" {
			t.Errorf("wrong content: %v", messages[0].Content)
		}
	})

	t.Run("merge assistant messages with tool calls", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleModel,
				Parts: []Part{
					Text("thinking..."),
					FuncCall{ID: "1", Name: "foo", Arguments: map[string]any{}},
				},
			},
			{
				Role: RoleModel,
				Parts: []Part{
					Text("more thinking..."),
				},
			},
		})
		messages, err := stateToOpenAIMessages(state)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 {
			t.Fatalf("expected 1 message, got %d: %+v", len(messages), messages)
		}
		if contentStr, ok := messages[0].Content.(string); !ok || contentStr != "thinking...more thinking..." {
			t.Errorf("wrong content: %v", messages[0].Content)
		}
		if len(messages[0].ToolCalls) != 1 {
			t.Errorf("wrong tool calls: %+v", messages[0].ToolCalls)
		}
	})

}

func TestAzureConfiguration(t *testing.T) {
	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		new(Module),
		modes.ForTest(t),
		&loader,
	).Call(func(
		newAzure NewAzure,
	) {
		g := newAzure(Spec{
			BaseURL:    "https://foo.openai.azure.com/",
			Model:      "my-deployment",
			APIVersion: "2024-05-01-preview",
			APIKey:     "my-key",
		})
		if g.spec.IsAzure == nil || !*g.spec.IsAzure {
			t.Fatal("IsAzure should be true")
		}
		if g.apiKey != "my-key" {
			t.Fatalf("wrong key: %s", g.apiKey)
		}
		if g.spec.APIVersion != "2024-05-01-preview" {
			t.Fatalf("wrong version: %s", g.spec.APIVersion)
		}
	})
}

func TestOpenAIStreamingPreservesPartialState(t *testing.T) {
	// Text longer than 64 chars triggers the parser to flush a content
	// chunk, causing AppendContent to be called during streaming.
	longText := strings.Repeat("a", 70)

	chunk1, _ := json.Marshal(ChatCompletionStreamResponse{
		Choices: []ChatCompletionStreamChoice{
			{Delta: ChatCompletionStreamChoiceDelta{Role: "assistant", Content: longText}},
		},
	})
	chunk2, _ := json.Marshal(ChatCompletionStreamResponse{
		Choices: []ChatCompletionStreamChoice{
			{Delta: ChatCompletionStreamChoiceDelta{Content: longText}},
		},
	})

	sseBody := fmt.Sprintf("data: %s\n\ndata: %s\n\ndata: [DONE]\n\n", chunk1, chunk2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseBody)
	}))
	defer server.Close()

	baseState := NewPrompts("", []*Content{
		{Role: RoleUser, Parts: []Part{Text("hi")}},
	})

	// Allow 1 successful AppendContent; the 2nd call (from the second
	// flushed chunk) will fail. The first chunk's content is already
	// in ret when the error occurs.
	failingState := &errorAfterNState{
		State:    baseState,
		maxCalls: 1,
	}

	disableTools := true
	openai := &OpenAI{
		spec: Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		},
		apiKey: "test-key",
		client: server.Client(),
	}
	openai.Logger = func() logs.Logger {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	openai.Tap = func() debugs.Tap {
		return func(context.Context, string, map[string]any) {}
	}

	ret, err := openai.Generate(context.Background(), failingState, nil)
	if err == nil {
		t.Fatal("expected error from failing AppendContent")
	}
	if ret == nil {
		t.Fatal("expected partial state to be preserved on error, got nil")
	}
}

func TestOpenAIErrorNoErrorField(t *testing.T) {
	// Regression: when the API returns a non-200 status with valid JSON
	// that lacks an "error" field, the code would panic with a nil pointer
	// dereference when trying to set errResp.Error.HTTPStatusCode.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message": "something went wrong"}`)
	}))
	defer server.Close()

	disableTools := true
	openai := &OpenAI{
		spec: Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		},
		apiKey: "test-key",
		client: server.Client(),
	}
	openai.Logger = func() logs.Logger {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	openai.Tap = func() debugs.Tap {
		return func(context.Context, string, map[string]any) {}
	}

	state := NewPrompts("", []*Content{
		{Role: RoleUser, Parts: []Part{Text("hi")}},
	})

	_, err := openai.Generate(context.Background(), state, nil)
	// Should return an error, not panic
	if err == nil {
		t.Fatal("expected error for non-200 status without error field")
	}
}
