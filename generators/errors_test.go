package generators

import (
	"errors"
	"testing"
)

func TestIsContextExceeded(t *testing.T) {
	matched := []string{
		`Error 400: This model's maximum context length is 128000 tokens. However, you requested 130000 tokens.`,
		"prompt is too long: 200100 tokens > 200000 maximum",
		"The input token count (900000) exceeds the maximum number of tokens allowed (819200).",
		"input length exceeds context window",
		"Request payload size exceeds the limit: 5242880 bytes. Please reduce it.",
		`bad status: 400, body: {"error":{"message":"your request contains too many tokens"}}`,
		"1210: 模型上下文长度超限",
		"Request Too Large",
	}
	for _, text := range matched {
		if !IsContextExceeded(errors.New(text)) {
			t.Errorf("expected match: %q", text)
		}
	}
	unmatched := []string{
		"Rate limit reached for requests: limit 30000 TPM",
		"invalid api key",
		"connection refused",
		"openai stream parse failed: unexpected end of JSON input",
	}
	for _, text := range unmatched {
		if IsContextExceeded(errors.New(text)) {
			t.Errorf("unexpected match: %q", text)
		}
	}
	if IsContextExceeded(nil) {
		t.Error("nil error must not match")
	}
	// The wrapped OpenAI error keeps the provider message in its text,
	// so the predicate classifies through the wrapper.
	openAIErr := OpenAIError{Err: errors.New(matched[0]), Request: ChatCompletionRequest{Model: "m"}}
	if !IsContextExceeded(openAIErr) {
		t.Error("expected OpenAIError to classify by its inner message")
	}
}
