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

func TestSpecZeroDataRetentionJSON(t *testing.T) {
	spec := Spec{
		Name:              "test",
		Type:              "gemini",
		ZeroDataRetention: new(true),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["zero_data_retention"]; !ok || v != true {
		t.Errorf("zero_data_retention not found or wrong: %v", raw)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ZeroDataRetention == nil || !*restored.ZeroDataRetention {
		t.Errorf("ZeroDataRetention not restored correctly: %+v", restored)
	}
}

func TestSpecRandomRedirectJSON(t *testing.T) {
	spec := Spec{
		Name:           "test",
		Type:           "gemini",
		RandomRedirect: []string{"/target1", "/target2"},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	v, ok := raw["random_redirect"]
	if !ok {
		t.Errorf("random_redirect not found in JSON")
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 2 {
		t.Errorf("expected 2 elements, got: %v", v)
	}

	// round trip
	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.RandomRedirect) != 2 ||
		restored.RandomRedirect[0] != "/target1" ||
		restored.RandomRedirect[1] != "/target2" {
		t.Errorf("RandomRedirect not restored correctly: %+v", restored)
	}
}
func TestSpecMaxInitTokensJSON(t *testing.T) {
	spec := Spec{
		Name:          "test",
		Type:          "gemini",
		MaxInitTokens: new(64000),
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["max_init_tokens"]; !ok || v != float64(64000) {
		t.Errorf("max_init_tokens not found or wrong: %v", raw)
	}

	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.MaxInitTokens == nil || *restored.MaxInitTokens != 64000 {
		t.Errorf("MaxInitTokens not restored correctly: %+v", restored)
	}
}

func TestSpecMaxTokensJSON(t *testing.T) {
	spec := Spec{
		Name:      "test",
		Type:      "gemini",
		MaxTokens: 192000,
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if v, ok := raw["max_tokens"]; !ok || v != float64(192000) {
		t.Fatalf("max_tokens not found or wrong: %v", raw)
	}
	if _, ok := raw["context_tokens"]; ok {
		t.Fatalf("the retired context_tokens key must not appear: %v", raw)
	}

	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.MaxTokens != 192000 {
		t.Fatalf("MaxTokens not restored correctly: %+v", restored)
	}
}

func TestSpecEffectiveMaxInitTokens(t *testing.T) {
	configured := Spec{MaxInitTokens: new(64000)}
	if got := configured.EffectiveMaxInitTokens(200000); got != 64000 {
		t.Errorf("configured generator must win, got %d", got)
	}
	unconfigured := Spec{}
	if got := unconfigured.EffectiveMaxInitTokens(200000); got != 200000 {
		t.Errorf("unconfigured generator must fall back to the global value, got %d", got)
	}
}
