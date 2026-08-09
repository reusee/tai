package generators

import (
	"encoding/json"
	"fmt"
)

const TheoryOfProviderRouting = `
Provider routing controls how a request is dispatched across the providers
that host a model. The Provider object mirrors the OpenRouter "provider"
parameter and is forwarded verbatim into the OpenAI-compatible request body
when the generator talks to OpenRouter. Fields use pointers or slices so
that "not set" is distinguishable from a zero value and omitted from the
serialized JSON. Spec resolution merges provider configurations field-wise:
a child spec's provider fields override the parent's, while unset child
fields preserve the parent's values.
`

// Provider holds routing preferences forwarded to OpenRouter. It mirrors
// the OpenRouter "provider" parameter (see TheoryOfProviderRouting).
type Provider struct {
	Order                  []string             `json:"order,omitempty"`
	AllowFallbacks         *bool                `json:"allow_fallbacks,omitempty"`
	RequireParameters      *bool                `json:"require_parameters,omitempty"`
	DataCollection         string               `json:"data_collection,omitempty"`
	ZDR                    *bool                `json:"zdr,omitempty"`
	EnforceDistillableText *bool                `json:"enforce_distillable_text,omitempty"`
	Only                   []string             `json:"only,omitempty"`
	Ignore                 []string             `json:"ignore,omitempty"`
	Quantizations          []string             `json:"quantizations,omitempty"`
	Sort                   *ProviderSort        `json:"sort,omitempty"`
	PreferredMinThroughput *ProviderPerformance `json:"preferred_min_throughput,omitempty"`
	PreferredMaxLatency    *ProviderPerformance `json:"preferred_max_latency,omitempty"`
	MaxPrice               *ProviderMaxPrice    `json:"max_price,omitempty"`
}

// ProviderSort orders provider endpoints by a strategy. It serializes as a
// plain string ("throughput") or as an object with "by" and "partition"
// fields, matching the OpenRouter provider.sort parameter.
type ProviderSort struct {
	By        string
	Partition string
}

func (s ProviderSort) MarshalJSON() ([]byte, error) {
	if s.Partition == "" {
		return json.Marshal(s.By)
	}
	return json.Marshal(struct {
		By        string `json:"by"`
		Partition string `json:"partition,omitempty"`
	}{
		By:        s.By,
		Partition: s.Partition,
	})
}

func (s *ProviderSort) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '"' {
		var by string
		if err := json.Unmarshal(data, &by); err != nil {
			return err
		}
		s.By = by
		return nil
	}
	var raw struct {
		By        string `json:"by"`
		Partition string `json:"partition"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.By = raw.By
	s.Partition = raw.Partition
	return nil
}

// ProviderPerformance is a throughput or latency preference. It can be a
// plain number (applied to the p50 percentile) or an object with
// percentile cutoffs (p50, p75, p90, p99), matching the OpenRouter
// preferred_min_throughput and preferred_max_latency parameters.
type ProviderPerformance struct {
	Value *float64
	P50   *float64
	P75   *float64
	P90   *float64
	P99   *float64
}

func (p ProviderPerformance) MarshalJSON() ([]byte, error) {
	if p.P50 == nil && p.P75 == nil && p.P90 == nil && p.P99 == nil {
		if p.Value == nil {
			return []byte("null"), nil
		}
		return json.Marshal(*p.Value)
	}
	return json.Marshal(struct {
		P50 *float64 `json:"p50,omitempty"`
		P75 *float64 `json:"p75,omitempty"`
		P90 *float64 `json:"p90,omitempty"`
		P99 *float64 `json:"p99,omitempty"`
	}{
		P50: p.P50,
		P75: p.P75,
		P90: p.P90,
		P99: p.P99,
	})
}

func (p *ProviderPerformance) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '{' {
		var raw struct {
			P50 *float64 `json:"p50"`
			P75 *float64 `json:"p75"`
			P90 *float64 `json:"p90"`
			P99 *float64 `json:"p99"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		p.P50 = raw.P50
		p.P75 = raw.P75
		p.P90 = raw.P90
		p.P99 = raw.P99
		return nil
	}
	var v float64
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid provider performance value: %w", err)
	}
	p.Value = &v
	return nil
}

// ProviderMaxPrice caps the per-token price the router may select,
// matching the OpenRouter provider.max_price parameter.
type ProviderMaxPrice struct {
	Prompt     *float64 `json:"prompt,omitempty"`
	Completion *float64 `json:"completion,omitempty"`
	Request    *float64 `json:"request,omitempty"`
	Image      *float64 `json:"image,omitempty"`
}

// merge overlays other onto p, returning the merged result. Field-by-field
// semantics: other's set fields win, p's unset fields survive.
func (p *Provider) merge(other *Provider) *Provider {
	if other == nil {
		return p
	}
	if p == nil {
		ret := *other
		return &ret
	}
	ret := *p
	if len(other.Order) > 0 {
		ret.Order = other.Order
	}
	if other.AllowFallbacks != nil {
		ret.AllowFallbacks = other.AllowFallbacks
	}
	if other.RequireParameters != nil {
		ret.RequireParameters = other.RequireParameters
	}
	if other.DataCollection != "" {
		ret.DataCollection = other.DataCollection
	}
	if other.ZDR != nil {
		ret.ZDR = other.ZDR
	}
	if other.EnforceDistillableText != nil {
		ret.EnforceDistillableText = other.EnforceDistillableText
	}
	if len(other.Only) > 0 {
		ret.Only = other.Only
	}
	if len(other.Ignore) > 0 {
		ret.Ignore = other.Ignore
	}
	if len(other.Quantizations) > 0 {
		ret.Quantizations = other.Quantizations
	}
	if other.Sort != nil {
		s := *other.Sort
		ret.Sort = &s
	}
	if other.PreferredMinThroughput != nil {
		s := *other.PreferredMinThroughput
		ret.PreferredMinThroughput = &s
	}
	if other.PreferredMaxLatency != nil {
		s := *other.PreferredMaxLatency
		ret.PreferredMaxLatency = &s
	}
	if other.MaxPrice != nil {
		s := *other.MaxPrice
		ret.MaxPrice = &s
	}
	return &ret
}
