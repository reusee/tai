package generators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
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
	t.Skip()
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

		messages, err := stateToOpenAIMessages(state, false)
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

		messages, err := stateToOpenAIMessages(state, false)
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
		messages, err := stateToOpenAIMessages(state, true)
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

	t.Run("thoughts filtered when not preserved", func(t *testing.T) {
		state := NewPrompts("", []*Content{
			{
				Role: RoleModel,
				Parts: []Part{
					Thought("thinking"),
					Text("answer"),
				},
			},
		})
		messages, err := stateToOpenAIMessages(state, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) != 1 {
			t.Fatalf("got %+v", messages)
		}
		if messages[0].ReasoningContent != "" {
			t.Errorf("thoughts should be filtered when not preserved, got reasoning: %s", messages[0].ReasoningContent)
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
		messages, err := stateToOpenAIMessages(state, false)
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

	// Construct OpenAI via dscope so all dscope.Inject fields are
	// properly initialized by dscope.InjectStruct, rather than manually
	// setting them with setTestOpenAIInjects. The nets.HTTPClient is
	// forked to point at the test server. See anytexts.TestContextPrompt
	// for the reference dscope test pattern.
	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.HTTPClient {
			return nets.HTTPClient{server.Client()}
		},
	).Call(func(
		newOpenAI NewOpenAI,
	) {
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
		openai := newOpenAI(Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		}, "test-key")

		ret, err := openai.Generate(context.Background(), failingState, nil)
		if err == nil {
			t.Fatal("expected error from failing AppendContent")
		}
		if ret == nil {
			t.Fatal("expected partial state to be preserved on error, got nil")
		}
	})
}

func TestOpenAIErrorNoErrorField(t *testing.T) {
	// When the API returns a non-200 status with valid JSON that lacks
	// an "error" field, the code must handle it without panicking.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message": "something went wrong"}`)
	}))
	defer server.Close()

	// Construct OpenAI via dscope so all dscope.Inject fields are
	// properly initialized by dscope.InjectStruct. The nets.HTTPClient
	// is forked to point at the test server. See anytexts.TestContextPrompt
	// for the reference dscope test pattern.
	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.HTTPClient {
			return nets.HTTPClient{server.Client()}
		},
	).Call(func(
		newOpenAI NewOpenAI,
	) {
		disableTools := true
		openai := newOpenAI(Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		}, "test-key")

		state := NewPrompts("", []*Content{
			{Role: RoleUser, Parts: []Part{Text("hi")}},
		})

		_, err := openai.Generate(context.Background(), state, nil)
		// Should return an error, not panic
		if err == nil {
			t.Fatal("expected error for non-200 status without error field")
		}
	})
}

func TestOpenAIRecordsAPIErrorThroughInjectedRecorder(t *testing.T) {
	// API errors are recorded through the dscope-injected EventRecorder,
	// not through a context value: the recorder is bound in the scope,
	// and the generator's Inject[EventRecorder] field receives it at
	// construction. See generators.TheoryOfEventRecorder.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message": "something went wrong"}`)
	}))
	defer server.Close()

	rec := &fakeEventRecorder{enabled: true}

	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.HTTPClient {
			return nets.HTTPClient{server.Client()}
		},
		func() EventRecorder { return rec },
	).Call(func(
		newOpenAI NewOpenAI,
	) {
		disableTools := true
		openai := newOpenAI(Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		}, "test-key")

		state := NewPrompts("", []*Content{
			{Role: RoleUser, Parts: []Part{Text("hi")}},
		})

		_, err := openai.Generate(context.Background(), state, nil)
		if err == nil {
			t.Fatal("expected error for non-200 status without error field")
		}
	})

	foundAPIError := false
	for _, e := range rec.events {
		if strings.HasPrefix(e, "api_error:") && strings.Contains(e, "openai http status 500") {
			foundAPIError = true
		}
	}
	if !foundAPIError {
		t.Fatalf("expected an api_error event recorded through the injected recorder, got %v", rec.events)
	}
}

func TestChatCompletionMessageUnmarshalArrayContent(t *testing.T) {
	var msg ChatCompletionMessage
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"text","text":"hello"},{"type":"image_url","image_url":{"url":"x"}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Role != "assistant" {
		t.Fatalf("expected role assistant, got %q", msg.Role)
	}
	if content, ok := msg.Content.(string); !ok || content != "hello" {
		t.Fatalf("expected content 'hello', got %#v", msg.Content)
	}

	// String content is preserved unchanged.
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"plain text"}`), &msg); err != nil {
		t.Fatal(err)
	}
	if content, ok := msg.Content.(string); !ok || content != "plain text" {
		t.Fatalf("expected content 'plain text', got %#v", msg.Content)
	}

	// Multiple text parts are concatenated in order.
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if content, ok := msg.Content.(string); !ok || content != "ab" {
		t.Fatalf("expected content 'ab', got %#v", msg.Content)
	}

	// No text parts: content becomes nil so the caller skips it.
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"x"}}]}`), &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Content != nil {
		t.Fatalf("expected nil content for non-text parts, got %#v", msg.Content)
	}
}

func TestOpenAINonStreamingArrayContent(t *testing.T) {
	// Some providers return non-streaming responses whose message content
	// is an array of content parts rather than a plain string. The text
	// parts must be captured; without the ChatCompletionMessage
	// normalization, the text is silently dropped and the caller receives
	// an empty response.
	response := ChatCompletionResponse{
		Choices: []ChatCompletionChoice{
			{
				Message: ChatCompletionMessage{
					Role:    "assistant",
					Content: []any{map[string]any{"type": "text", "text": "hello from array"}},
				},
				FinishReason: "stop",
			},
		},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer server.Close()

	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.HTTPClient {
			return nets.HTTPClient{server.Client()}
		},
	).Call(func(
		newOpenAI NewOpenAI,
	) {
		disableTools := true
		openai := newOpenAI(Spec{
			BaseURL:      server.URL,
			Model:        "test-model",
			DisableTools: &disableTools,
		}, "test-key")

		state := NewPrompts("", []*Content{
			{Role: RoleUser, Parts: []Part{Text("hi")}},
		})

		newState, err := openai.Generate(context.Background(), state, &GenerateOptions{
			NonStreaming: true,
		})
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for c := range newState.Contents() {
			for _, p := range c.Parts {
				if text, ok := p.(Text); ok && strings.Contains(string(text), "hello from array") {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("expected array-form content to be captured as text")
		}
	})
}

func TestTemperatureAndMaxTokensOmittedWhenNotSet(t *testing.T) {
	// When neither temperature nor max_completion_tokens is specified
	// (nil pointers), both must be omitted from the request JSON so the
	// API uses its own defaults.
	req := ChatCompletionRequest{
		Model: "test-model",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"temperature"`) {
		t.Fatalf("temperature should be omitted when not set, got: %s", string(data))
	}
	if strings.Contains(string(data), `"max_completion_tokens"`) {
		t.Fatalf("max_completion_tokens should be omitted when not set, got: %s", string(data))
	}
}

func TestTemperatureZeroIncludedInJSON(t *testing.T) {
	// Temperature 0 must be included in the request JSON. omitempty
	// would omit it, causing the API to fall back to its default
	// (typically 1.0) instead of the intended deterministic temperature 0.
	req := ChatCompletionRequest{
		Model:       "test-model",
		Temperature: new(float32(0)),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"temperature":0`) {
		t.Fatalf("temperature 0 must be included in JSON, got: %s", string(data))
	}
}
