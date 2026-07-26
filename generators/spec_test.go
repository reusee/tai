package generators

import (
	"encoding/json"
	"testing"
)

func TestSpecNoProxy(t *testing.T) {
	spec := Spec{
		Name:    "test",
		Type:    "gemini",
		Model:   "gemini-flash",
		NoProxy: new(true),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["no_proxy"]; !ok || v != true {
		t.Errorf("no_proxy not found or wrong: %v", raw)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.NoProxy == nil || !*restored.NoProxy {
		t.Errorf("NoProxy not restored correctly: %+v", restored)
	}
}

func TestSpecMaxThinkingTokensJSON(t *testing.T) {
	spec := Spec{
		Name:              "test",
		Type:              "gemini",
		MaxThinkingTokens: new(5000),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["max_thinking_tokens"]; !ok || v != float64(5000) {
		t.Errorf("max_thinking_tokens not found or wrong: %v", raw)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.MaxThinkingTokens == nil || *restored.MaxThinkingTokens != 5000 {
		t.Errorf("MaxThinkingTokens not restored correctly: %+v", restored)
	}
}

func TestSpecPreservedThinkingJSON(t *testing.T) {
	spec := Spec{
		Name:              "test",
		Type:              "gemini",
		PreservedThinking: new(true),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["preserved_thinking"]; !ok || v != true {
		t.Errorf("preserved_thinking not found or wrong: %v", raw)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.PreservedThinking == nil || !*restored.PreservedThinking {
		t.Errorf("PreservedThinking not restored correctly: %+v", restored)
	}
}
