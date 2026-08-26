package generators

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"google.golang.org/genai"
)

func TestGemini(t *testing.T) {
	t.Skip()
	testGenerator(t, func(
		newGemini NewGemini,
	) Generator {
		generator := newGemini(Spec{
			Model:             "models/gemini-flash-latest",
			ContextTokens:     1 * M,
			MaxGenerateTokens: new(64 * K),
			Temperature:       new(float32(0.1)),
			DisableSearch:     new(true),
		})
		return generator
	})
}

func TestGeminiListModels(t *testing.T) {
	t.Skip()
	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() nets.ProxyAddr {
			return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
		},
	).Call(func(
		httpClient nets.HTTPClient,
		apiKey GoogleAPIKey,
	) {
		ctx := t.Context()

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     string(apiKey),
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: httpClient.Client,
		})
		if err != nil {
			t.Fatal(err)
		}

		resp, err := client.Models.List(ctx, &genai.ListModelsConfig{
			PageSize: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range resp.Items {
			_ = model
		}

	})
}

type geminiMockTransport struct {
	body string
}

func (t *geminiMockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(strings.NewReader(t.body)),
	}, nil
}

type immutableErrorAfterNState struct {
	State
	calls    int
	maxCalls int
}

func (s *immutableErrorAfterNState) AppendContent(content *Content) (State, error) {
	if s.calls >= s.maxCalls {
		return s, errors.New("append content failed")
	}
	inner, err := s.State.AppendContent(content)
	if err != nil {
		return s, err
	}
	return &immutableErrorAfterNState{
		State:    inner,
		calls:    s.calls + 1,
		maxCalls: s.maxCalls,
	}, nil
}

func TestGeminiStreamingPreservesPartialState(t *testing.T) {
	// Create mock SSE responses in Gemini format. Each data line is a
	// GenerateContentResponse JSON object with a candidate containing text.
	chunk1, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{
						{"text": strings.Repeat("a", 100)},
					},
				},
			},
		},
	})
	chunk2, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{
						{"text": strings.Repeat("b", 100)},
					},
				},
			},
		},
	})
	sseBody := fmt.Sprintf("data: %s\n\ndata: %s\n\n", chunk1, chunk2)

	mockTransport := &geminiMockTransport{body: sseBody}

	dscope.New(
		modes.ForTest(t),
		new(Module),
	).Fork(
		func() nets.HTTPClient {
			return nets.HTTPClient{
				Client: &http.Client{Transport: mockTransport},
			}
		},
	).Call(func(
		newGemini NewGemini,
	) {
		baseState := NewPrompts("", []*Content{
			{Role: RoleUser, Parts: []Part{Text("hi")}},
		})

		// immutableErrorAfterNState returns a new instance on each
		// successful AppendContent, preserving State immutability.
		// maxCalls=1 allows the first chunk's content to be appended;
		// the second chunk's AppendContent fails. The first chunk's
		// content is in newState when the error occurs.
		failingState := &immutableErrorAfterNState{
			State:    baseState,
			maxCalls: 1,
		}

		inputCount := CountContents(failingState)
		disableSearch := true
		disableTools := true
		gemini := newGemini(Spec{
			Model:         "test-model",
			APIKey:        "test-key",
			DisableSearch: &disableSearch,
			DisableTools:  &disableTools,
		})

		ret, err := gemini.Generate(context.Background(), failingState, nil)
		if err == nil {
			t.Fatal("expected error from failing AppendContent")
		}
		if ret == nil {
			t.Fatal("expected partial state to be preserved on error, got nil")
		}
		retCount := CountContents(ret)
		if retCount <= inputCount {
			t.Fatalf("expected partial state to have more content than input (%d > %d)", retCount, inputCount)
		}
	})
}
