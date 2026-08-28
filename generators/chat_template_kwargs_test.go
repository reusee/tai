package generators

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestChatCompletionRequestChatTemplateKwargs locks the wire format of the
// chat_template_kwargs request field: present when set, omitted when nil.
func TestChatCompletionRequestChatTemplateKwargs(t *testing.T) {
	req := ChatCompletionRequest{
		Model:              "m",
		ChatTemplateKwargs: map[string]any{"preserve_thinking": true},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"chat_template_kwargs":{"preserve_thinking":true}`) {
		t.Fatalf("expected chat_template_kwargs in %s", data)
	}

	data, err = json.Marshal(ChatCompletionRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "chat_template_kwargs") {
		t.Fatalf("expected no chat_template_kwargs in %s", data)
	}
}
