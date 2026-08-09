package generators

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProviderSortJSON(t *testing.T) {
	// string form
	data, err := json.Marshal(ProviderSort{By: "throughput"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"throughput"` {
		t.Fatalf("got %s", data)
	}

	// object form
	data, err = json.Marshal(ProviderSort{By: "throughput", Partition: "none"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["by"] != "throughput" || raw["partition"] != "none" {
		t.Fatalf("got %v", raw)
	}

	// unmarshal string form
	var s ProviderSort
	if err := json.Unmarshal([]byte(`"price"`), &s); err != nil {
		t.Fatal(err)
	}
	if s.By != "price" || s.Partition != "" {
		t.Fatalf("got %+v", s)
	}

	// unmarshal object form
	if err := json.Unmarshal([]byte(`{"by":"latency","partition":"model"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.By != "latency" || s.Partition != "model" {
		t.Fatalf("got %+v", s)
	}
}

func TestProviderPerformanceJSON(t *testing.T) {
	// number form
	v := 50.0
	data, err := json.Marshal(ProviderPerformance{Value: &v})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "50" {
		t.Fatalf("got %s", data)
	}

	// object form
	p90 := 3.0
	data, err = json.Marshal(ProviderPerformance{P90: &p90})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"p90":3`) {
		t.Fatalf("got %s", data)
	}

	// unmarshal number form
	var p ProviderPerformance
	if err := json.Unmarshal([]byte(`50`), &p); err != nil {
		t.Fatal(err)
	}
	if p.Value == nil || *p.Value != 50 {
		t.Fatalf("got %+v", p)
	}

	// unmarshal object form
	if err := json.Unmarshal([]byte(`{"p90": 3}`), &p); err != nil {
		t.Fatal(err)
	}
	if p.P90 == nil || *p.P90 != 3 {
		t.Fatalf("got %+v", p)
	}
}

func TestProviderJSONRoundTrip(t *testing.T) {
	allowFallbacks := false
	zdr := true
	float64Ptr := func(v float64) *float64 { return &v }

	provider := &Provider{
		Order:          []string{"anthropic", "openai"},
		AllowFallbacks: &allowFallbacks,
		ZDR:            &zdr,
		Sort:           &ProviderSort{By: "throughput", Partition: "none"},
		PreferredMaxLatency: &ProviderPerformance{
			P90: float64Ptr(3),
		},
		PreferredMinThroughput: &ProviderPerformance{
			Value: float64Ptr(50),
		},
		MaxPrice: &ProviderMaxPrice{
			Prompt: float64Ptr(1),
		},
	}

	spec := Spec{
		Name:     "test",
		Type:     "openrouter",
		Provider: provider,
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	var restored Spec
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}

	if restored.Provider == nil {
		t.Fatal("provider is nil after round trip")
	}
	if len(restored.Provider.Order) != 2 || restored.Provider.Order[0] != "anthropic" {
		t.Fatalf("unexpected order: %v", restored.Provider.Order)
	}
	if restored.Provider.AllowFallbacks == nil || *restored.Provider.AllowFallbacks {
		t.Fatalf("unexpected allow_fallbacks: %v", restored.Provider.AllowFallbacks)
	}
	if restored.Provider.ZDR == nil || !*restored.Provider.ZDR {
		t.Fatalf("unexpected zdr: %v", restored.Provider.ZDR)
	}
	if restored.Provider.Sort == nil || restored.Provider.Sort.By != "throughput" || restored.Provider.Sort.Partition != "none" {
		t.Fatalf("unexpected sort: %+v", restored.Provider.Sort)
	}
	if restored.Provider.PreferredMaxLatency == nil || restored.Provider.PreferredMaxLatency.P90 == nil || *restored.Provider.PreferredMaxLatency.P90 != 3 {
		t.Fatalf("unexpected preferred_max_latency: %+v", restored.Provider.PreferredMaxLatency)
	}
	if restored.Provider.PreferredMinThroughput == nil || restored.Provider.PreferredMinThroughput.Value == nil || *restored.Provider.PreferredMinThroughput.Value != 50 {
		t.Fatalf("unexpected preferred_min_throughput: %+v", restored.Provider.PreferredMinThroughput)
	}
	if restored.Provider.MaxPrice == nil || restored.Provider.MaxPrice.Prompt == nil || *restored.Provider.MaxPrice.Prompt != 1 {
		t.Fatalf("unexpected max_price: %+v", restored.Provider.MaxPrice)
	}
}

func TestResolveSpecProviderMerge(t *testing.T) {
	parentOnly := true
	allowFallbacks := false
	roots := []Spec{
		{
			Name: "base",
			Type: "openrouter",
			Provider: &Provider{
				Order:          []string{"anthropic"},
				AllowFallbacks: &allowFallbacks,
				ZDR:            &parentOnly,
			},
			Variants: []Spec{
				{
					Name: "child",
					Type: "openrouter",
					Provider: &Provider{
						Order: []string{"openai", "together"},
					},
				},
			},
		},
	}

	s, err := resolveSpec("base/child", roots)
	if err != nil {
		t.Fatal(err)
	}
	if s.Provider == nil {
		t.Fatal("provider is nil")
	}
	// child's order overrides parent's
	if len(s.Provider.Order) != 2 || s.Provider.Order[0] != "openai" {
		t.Fatalf("unexpected order: %v", s.Provider.Order)
	}
	// parent's unset fields survive
	if s.Provider.AllowFallbacks == nil || *s.Provider.AllowFallbacks {
		t.Fatalf("expected allow_fallbacks false from parent, got %v", s.Provider.AllowFallbacks)
	}
	if s.Provider.ZDR == nil || !*s.Provider.ZDR {
		t.Fatalf("expected zdr true from parent, got %v", s.Provider.ZDR)
	}
}

func TestChatCompletionRequestProviderJSON(t *testing.T) {
	zdr := true
	req := ChatCompletionRequest{
		Model: "test-model",
		Provider: &Provider{
			ZDR:  &zdr,
			Sort: &ProviderSort{By: "price", Partition: "none"},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"provider":{`) {
		t.Fatalf("provider missing from request JSON: %s", data)
	}
	if !strings.Contains(string(data), `"zdr":true`) {
		t.Fatalf("zdr missing from request JSON: %s", data)
	}
	if !strings.Contains(string(data), `"sort":{`) {
		t.Fatalf("sort missing from request JSON: %s", data)
	}
}

func TestChatCompletionRequestProviderOmittedWhenNil(t *testing.T) {
	req := ChatCompletionRequest{Model: "test-model"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"provider"`) {
		t.Fatalf("provider should be omitted when nil: %s", data)
	}
}
