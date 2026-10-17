package generators

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
)

const TheoryOfJev = `
The Jev generator speaks the TypeSafe System One protocol: one POST to
<base_url>/systemone carries the state and a map of typed questions, and
the response carries one typed answer per question. Jev produces no text
and streams nothing, so the Generator contract maps as follows.

State: the model-facing conversation becomes the System One state, an
object carrying the system prompt and one message per content. Log and
system contents are skipped, and thoughts travel only when
PreservedThinking is set, the same rules the OpenAI request path applies.

Questions: the declared functions are the questions. Each function
becomes one noul question keyed by the function name, asking whether that
function handles the latest user message; the function's name and
description travel as structured instructions. Every answer at or above
the selection threshold selects its function, and a selected function
becomes a function call carrying the question id as its call id and no
arguments: Jev decides whether a function applies, not what its
arguments are, and the executor reports a missing argument back into the
conversation like any other tool error.

A state without declared functions has nothing to ask and fails before
the HTTP call: Jev answers typed questions, so a missing question is a
caller fault, not a transport fault.

The answers are appended as log content, so every probability and
confidence stays visible without a second request. Usage maps the API
counts onto the usage part: input tokens are the prompt count and output
tokens are the generated count. Rate limiting and overload (429 and 529)
join ErrRetryable, so the generate phase retries them like every other
provider.
`

// jevSelectionThreshold is the noul value at or above which an answer
// selects its function. Half is the point where yes and no weigh the same.
// See TheoryOfJev.
const jevSelectionThreshold = 0.5

// SystemOneRequest is the body of one System One evaluation request: the
// state to evaluate, the model that evaluates it, and the typed questions.
// See TheoryOfJev.
type SystemOneRequest struct {
	State     SystemOneState               `json:"state"`
	Model     string                       `json:"model"`
	Questions map[string]SystemOneQuestion `json:"questions"`
}

// SystemOneQuestion is one typed question: its type, its instructions, and,
// for choice and score questions, its criteria. See TheoryOfJev.
type SystemOneQuestion struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// SystemOneState is the state of a System One request: the system prompt
// and one message per model-facing content. See TheoryOfJev.
type SystemOneState struct {
	System   string             `json:"system,omitempty"`
	Messages []SystemOneMessage `json:"messages,omitempty"`
}

// SystemOneMessage is one message of a System One state. See TheoryOfJev.
type SystemOneMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// SystemOneResponse is the body of one System One response: the answering
// model, one answer per question, and the token usage. See TheoryOfJev.
type SystemOneResponse struct {
	Model   string                     `json:"model"`
	Answers map[string]SystemOneAnswer `json:"answers"`
	Usage   *SystemOneUsage            `json:"usage,omitempty"`
}

// SystemOneAnswer is one typed answer. The type field selects which value
// fields carry the answer. See TheoryOfJev.
type SystemOneAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]any     `json:"legend,omitempty"`
}

// SystemOneUsage reports the token counts of one System One request. See
// TheoryOfJev.
type SystemOneUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// JevError reports a failed System One request. The request stays out of
// the error text, so phrase-based classification such as
// IsContextExceeded reads the provider's own report, never an echoed
// request. See TheoryOfContextExceeded.
type JevError struct {
	Err     error
	Request SystemOneRequest
}

func (e JevError) Error() string {
	return e.Err.Error()
}

// Jev is a Generator backed by the TypeSafe System One API. See
// TheoryOfJev.
type Jev struct {
	spec   Spec
	apiKey string
	client nets.HTTPClient

	Count         dscope.Inject[BPETokenCounter]
	Logger        dscope.Inject[logs.Logger]
	EventRecorder dscope.Inject[EventRecorder]
	FuncDecls     dscope.Inject[FuncDecls]
}

var _ Generator = Jev{}

func (j Jev) Spec() Spec {
	return j.spec
}

// CountTokens estimates the token count with the shared BPE counter:
// TypeSafe publishes no tokenizer, and the exact counts of a request come
// back in its usage. See TheoryOfJev.
func (j Jev) CountTokens(text string) (int, error) {
	return j.Count()(text)
}

// recordEvent records an API-level event (api_error) in the scope's
// generators.EventSink: the per-scope EventSink is the default
// EventRecorder, injected as a dscope dependency when the generator is
// constructed, and the generation loop drains the sink after each round,
// recording every buffered event as a session-tree event node. See
// generators.TheoryOfEventRecorder.
func (j Jev) recordEvent(typ string, detail string) {
	if rec := j.EventRecorder(); rec != nil && rec.Enabled() {
		rec.Event(typ, detail)
	}
}

// NewJev builds a Jev generator from a spec. The HTTP client and the
// TypeSafe API key arrive from the scope, so proxy handling stays uniform
// with every other generator. The spec's own API key takes precedence and
// falls back to the configured key; the provider defaults the base URL to
// the public System One endpoint. See TheoryOfJev.
type NewJev func(spec Spec) Jev

func (Module) NewJev(
	inject dscope.InjectStruct,
	client nets.HTTPClient,
	apiKey TypeSafeAPIKey,
) NewJev {
	return func(spec Spec) Jev {
		if spec.BaseURL == "" {
			spec.BaseURL = "https://api.typesafe.ai/v1"
		}
		ret := Jev{
			spec:   spec,
			apiKey: firstNonZero(spec.APIKey, string(apiKey)),
			client: client,
		}
		inject(&ret)
		return ret
	}
}

func (j Jev) Generate(ctx context.Context, state State, options *GenerateOptions) (ret State, err error) {
	ret = state

	preservedThinking := j.spec.PreservedThinking != nil && *j.spec.PreservedThinking
	var funcs []FuncDecl
	if j.spec.DisableTools == nil || !*j.spec.DisableTools {
		funcs = jevFunctions(ret, j.FuncDecls())
	}
	if len(funcs) == 0 {
		j.Logger().WarnContext(ctx, "jev request without questions",
			"name", j.spec.Name,
			"model", j.spec.Model,
			"disable_tools", j.spec.DisableTools != nil && *j.spec.DisableTools,
		)
		return ret, fmt.Errorf("jev: no questions to ask: declare the functions to evaluate in the state or in the func_decls config")
	}

	stateValue := jevState(ret, preservedThinking)
	request := SystemOneRequest{
		State:     stateValue,
		Model:     j.spec.Model,
		Questions: jevQuestions(funcs),
	}

	body, err := json.Marshal(request)
	if err != nil {
		j.Logger().WarnContext(ctx, "marshalling jev request failed",
			"name", j.spec.Name,
			"model", j.spec.Model,
			"error", err,
		)
		return ret, err
	}

	j.Logger().InfoContext(ctx, "jev request built",
		"name", j.spec.Name,
		"model", j.spec.Model,
		"base_url", j.spec.BaseURL,
		"api_key_set", j.apiKey != "",
		"questions", len(request.Questions),
		"messages", len(stateValue.Messages),
		"body_bytes", len(body),
		"preserved_thinking", preservedThinking,
	)

	url := strings.TrimSuffix(j.spec.BaseURL, "/") + "/systemone"

	// Select HTTP client based on NoProxy flag
	client := j.client
	if j.spec.NoProxy != nil && *j.spec.NoProxy {
		client = nets.HTTPClient{
			Client: &http.Client{
				Transport: &http.Transport{
					DialContext: (&net.Dialer{}).DialContext,
				},
			},
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		j.Logger().WarnContext(ctx, "creating jev http request failed",
			"model", j.spec.Model,
			"url", url,
			"error", err,
		)
		return ret, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+j.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	j.Logger().InfoContext(ctx, "sending jev request",
		"model", j.spec.Model,
		"url", url,
		"no_proxy", j.spec.NoProxy != nil && *j.spec.NoProxy,
		"body_bytes", len(body),
	)

	// Request timing: TotalDuration spans from sending the request to the
	// end of the response. The protocol streams nothing, so there is no
	// time to first token. See TheoryOfUsageTiming.
	requestStarted := time.Now()

	resp, err := client.Do(httpReq)
	if err != nil {
		j.Logger().WarnContext(ctx, "jev request failed",
			"model", j.spec.Model,
			"url", url,
			"error", err,
		)
		j.recordEvent("api_error", fmt.Sprintf("jev request failed: %v", err))
		return ret, JevError{
			Err:     err,
			Request: request,
		}
	}
	defer resp.Body.Close()

	j.Logger().InfoContext(ctx, "jev response status",
		"model", j.spec.Model,
		"status", resp.StatusCode,
		"status_text", resp.Status,
		"content_type", resp.Header.Get("Content-Type"),
	)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		j.Logger().WarnContext(ctx, "reading jev response failed",
			"model", j.spec.Model,
			"error", err,
		)
		j.recordEvent("api_error", fmt.Sprintf("jev response read failed: %v", err))
		return ret, err
	}

	if resp.StatusCode != http.StatusOK {
		statusMsg := fmt.Sprintf("jev http status %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
		j.Logger().WarnContext(ctx, "jev http status",
			"model", j.spec.Model,
			"status", resp.StatusCode,
			"body", strings.TrimSpace(string(responseBody)),
		)
		j.recordEvent("api_error", statusMsg)
		apiErr := JevError{
			Err:     fmt.Errorf("bad status: %d, body: %s", resp.StatusCode, strings.TrimSpace(string(responseBody))),
			Request: request,
		}
		// Rate limiting (429) and overload (529) are transient faults:
		// join ErrRetryable so the generate phase retries them.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529 {
			return ret, errors.Join(apiErr, ErrRetryable)
		}
		return ret, apiErr
	}

	var response SystemOneResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		j.Logger().WarnContext(ctx, "unmarshalling jev response failed",
			"model", j.spec.Model,
			"error", err,
		)
		j.recordEvent("api_error", fmt.Sprintf("jev response unmarshal failed: %v", err))
		return ret, JevError{
			Err:     err,
			Request: request,
		}
	}

	j.Logger().InfoContext(ctx, "jev response received",
		"model", response.Model,
		"answers", len(response.Answers),
	)

	if response.Usage != nil {
		var usage Usage
		usage.Prompt.TokenCount = response.Usage.InputTokens
		usage.Candidates.TokenCount = response.Usage.OutputTokens
		usage.TotalDuration = time.Since(requestStarted)
		j.Logger().InfoContext(ctx, "jev usage",
			"model", response.Model,
			"prompt", usage.Prompt.TokenCount,
			"completion", usage.Candidates.TokenCount,
			"total_duration", usage.TotalDuration,
		)
		if ret, err = ret.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{usage},
		}); err != nil {
			return ret, err
		}
	}

	selected := jevSelectedFunctions(response.Answers)
	if len(selected) > 0 {
		content := &Content{
			Role: RoleModel,
		}
		for _, name := range selected {
			content.Parts = append(content.Parts, FuncCall{
				ID:        name,
				Name:      name,
				Arguments: map[string]any{},
			})
		}
		j.Logger().InfoContext(ctx, "jev functions selected",
			"model", response.Model,
			"calls", len(selected),
		)
		if ret, err = ret.AppendContent(content); err != nil {
			return ret, err
		}
	}

	answersJSON, err := json.Marshal(response.Answers)
	if err != nil {
		j.Logger().WarnContext(ctx, "marshalling jev answers failed",
			"model", response.Model,
			"error", err,
		)
		return ret, JevError{
			Err:     err,
			Request: request,
		}
	}
	if ret, err = ret.AppendContent(&Content{
		Role: RoleLog,
		Parts: []Part{
			Text(fmt.Sprintf("jev answers from %s: %s", response.Model, answersJSON)),
		},
	}); err != nil {
		return ret, err
	}

	if ret, err = ret.Flush(); err != nil {
		j.Logger().WarnContext(ctx, "flushing jev state failed",
			"model", j.spec.Model,
			"error", err,
		)
		return ret, err
	}

	j.Logger().InfoContext(ctx, "jev generate done",
		"model", response.Model,
		"content_count", CountContents(ret),
	)

	return ret, nil
}

// jevState renders the model-facing conversation as the System One state,
// applying the same rules as the OpenAI request path: log and system
// contents are skipped, and thoughts travel only with PreservedThinking.
// See TheoryOfJev.
func jevState(state State, preservedThinking bool) SystemOneState {
	ret := SystemOneState{
		System: state.SystemPrompt(),
	}
	for content := range state.Contents() {
		if content.Role == RoleLog || content.Role == RoleSystem {
			continue
		}
		var texts []string
		for _, part := range content.Parts {
			switch part := part.(type) {
			case Text:
				if len(part) > 0 {
					texts = append(texts, string(part))
				}
			case Thought:
				if preservedThinking && len(part) > 0 {
					texts = append(texts, string(part))
				}
			case FileURL:
				if len(part) > 0 {
					texts = append(texts, string(part))
				}
			case FileContent:
				if isTextMIMEType(part.MimeType) && len(part.Content) > 0 {
					texts = append(texts, string(part.Content))
				}
			}
		}
		if len(texts) == 0 {
			continue
		}
		ret.Messages = append(ret.Messages, SystemOneMessage{
			Role: string(content.Role),
			Text: strings.Join(texts, "\n"),
		})
	}
	return ret
}

// jevFunctions collects the declared functions: the state's own functions
// first, then the configured declarations, deduplicated by name and sorted
// by name so equal states produce equal requests. See TheoryOfPrefixCaching.
func jevFunctions(state State, decls FuncDecls) []FuncDecl {
	seen := make(map[string]bool)
	var ret []FuncDecl
	for fn := range state.Functions() {
		if fn.Decl.Name == "" || seen[fn.Decl.Name] {
			continue
		}
		seen[fn.Decl.Name] = true
		ret = append(ret, fn.Decl)
	}
	for _, decl := range decls {
		if decl.Name == "" || seen[decl.Name] {
			continue
		}
		seen[decl.Name] = true
		ret = append(ret, decl)
	}
	sort.SliceStable(ret, func(i, j int) bool {
		return ret[i].Name < ret[j].Name
	})
	return ret
}

// jevQuestions turns the declared functions into the request's questions:
// one noul question per function, keyed by the function name. See
// TheoryOfJev.
func jevQuestions(funcs []FuncDecl) map[string]SystemOneQuestion {
	ret := make(map[string]SystemOneQuestion, len(funcs))
	for _, fn := range funcs {
		target := map[string]any{
			"name": fn.Name,
		}
		if fn.Description != "" {
			target["description"] = fn.Description
		}
		ret[fn.Name] = SystemOneQuestion{
			Type: "noul",
			Instructions: map[string]any{
				"function": target,
				"question": "Does this function handle the latest user message?",
			},
			Criteria: map[string]any{
				"true":  "the function handles the latest user message",
				"false": "another function or a plain answer handles the latest user message",
			},
		}
	}
	return ret
}

// jevSelectedFunctions returns the names of the functions whose noul answer
// reaches the selection threshold, ordered by descending probability and
// then by name, so equal answers produce equal output. See TheoryOfJev.
func jevSelectedFunctions(answers map[string]SystemOneAnswer) []string {
	type selection struct {
		name string
		noul float64
	}
	var selections []selection
	for name, answer := range answers {
		if answer.Type != "noul" || answer.Noul == nil {
			continue
		}
		if *answer.Noul < jevSelectionThreshold {
			continue
		}
		selections = append(selections, selection{
			name: name,
			noul: *answer.Noul,
		})
	}
	sort.Slice(selections, func(i, j int) bool {
		if selections[i].noul != selections[j].noul {
			return selections[i].noul > selections[j].noul
		}
		return selections[i].name < selections[j].name
	})
	var ret []string
	for _, selection := range selections {
		ret = append(ret, selection.name)
	}
	return ret
}
