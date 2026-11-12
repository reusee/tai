package generators

import (
	"bytes"
	"context"
	"encoding/base64"
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

const TheoryOfDecisions = `
Decisions is the Gemini typed-question protocol: one POST to
<base_url>/decisions carries shared input and a map of typed questions
(noul, choice, score) and returns one calibrated answer per question,
generating no text and billing no output tokens. It is the second
implementation of the typed-question model this package already speaks for
System One (see TheoryOfSystemOne): a question map in, an answer map out,
nothing generated. The two differ only in transport and in question
vocabulary, so this generator mirrors SystemOne's contract mapping instead
of inventing one.

The protocol is a generator type, not a mode of the Gemini content
generator: Type is the only protocol selector (see TheoryOfSpec). It reuses
the Gemini credentials and the Gemini Developer API root, because the
Decisions API is a Gemini Developer API service.

GenerateOptions carry no meaning here: the protocol streams nothing, takes
no response schema, and generates no tokens, so a request ignores them.

Questions: the declared functions are the questions, one noul question per
function asking whether that function handles the latest user message,
exactly as System One asks them. The protocol reads text instructions only
and never sees question keys, so the function name and description travel
inside the instruction text. A question key admits only letters, digits,
underscores, and dashes while a function name may carry dots and colons, so
each key is derived from its function name and disambiguated on collision,
and the caller keeps the key-to-name map so every answer maps back to its
function.

Answers: every noul answer at or above the selection threshold selects its
function, ordered by descending probability and then by key, and each
selected function becomes a function call carrying no arguments — decisions
decides whether a function applies, not what its arguments are, and the
executor reports a missing argument back into the conversation like any
other tool error. The full answer map, with every probability, confidence,
score, and legend, is appended as log content, so the calibrated numbers
stay readable without a second request.

Input: the protocol has no system instruction field, so the state's system
prompt becomes the input's first text block. Each content contributes its
text as text blocks, a text MIME file its bytes as text, and an image,
audio, video, or PDF file a typed media block carrying base64 bytes and the
real MIME type. A file URL travels as its URI text, and a file with neither
a recognized media type nor a text MIME type is dropped — the same rules
System One applies. Text parts join into one block per run and media blocks
keep their position. The protocol concatenates adjacent text blocks with no
separator, so every text block ends with a blank line: text fragments join
verbatim, and a file URL or a text file joins the input as its own unit,
every unit separated by a blank line (see TheoryOfContentUnitSeparation).
Thoughts, tool calls, and tool results stay out: the protocol takes none of
them, and a caller that needs them in the input serializes them as text.

A state without declared functions has nothing to ask and fails before the
HTTP call: the protocol answers typed questions, so a missing question is a
caller fault, not a transport fault.

Rate limiting and server errors join ErrRetryable so the generate phase
retries them. A transport fault arrives wrapped in DecisionsError, which
unwraps to it, so the shared classifier still sees the network fault
through the wrapper (see TheoryOfTransientTransportErrors). A rejected
request carries the API's own error code, which names a safety block, and
that message is the provider's report that phrase classification such as
IsContextExceeded reads (see TheoryOfContextExceeded).
`

// decisionsBaseURL is the Gemini Developer API root that serves the Decisions
// API. See TheoryOfDecisions.
const decisionsBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// decisionsModel is the only model the Decisions API serves. See
// TheoryOfDecisions.
const decisionsModel = "gemini-decision-1"

// decisionsSelectionThreshold is the noul value at or above which an answer
// selects its function. Half is the point where yes and no weigh the same,
// the same point System One uses. See TheoryOfDecisions.
const decisionsSelectionThreshold = 0.5

// decisionsMaxQuestionKey caps a derived question key at the protocol's key
// length. A longer name is truncated before the collision suffix is appended.
// See TheoryOfDecisions.
const decisionsMaxQuestionKey = 64

// DecisionsRequest is the body of one decisions request: the shared input,
// the model that evaluates it, and the typed questions keyed by a derived
// question key. See TheoryOfDecisions.
type DecisionsRequest struct {
	Model     string                       `json:"model"`
	Input     []DecisionsContent           `json:"input"`
	Questions map[string]DecisionsQuestion `json:"questions"`
}

// DecisionsContent is one input block: a text block carries text, a media
// block carries base64 bytes and the real MIME type the API names its block
// after. See TheoryOfDecisions.
type DecisionsContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
}

// DecisionsQuestion is one typed question: its type, its instruction text,
// and, for choice and score questions, its criteria. See TheoryOfDecisions.
type DecisionsQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// DecisionsResponse is the body of one decisions response: one answer per
// question under the question key, and the token usage. See
// TheoryOfDecisions.
type DecisionsResponse struct {
	Answers map[string]DecisionsAnswer `json:"answers"`
	Usage   *DecisionsUsage            `json:"usage,omitempty"`
}

// DecisionsAnswer is one typed answer. The type field selects which value
// fields carry the answer, and confidence reports how likely the top answer
// is right. See TheoryOfDecisions.
type DecisionsAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// DecisionsUsage reports the token counts of one decisions request: the
// protocol bills input only, so the output count is always zero. See
// TheoryOfDecisions.
type DecisionsUsage struct {
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
	TotalTokens       int `json:"total_tokens"`
}

// DecisionsAPIError is the error object the API returns in place of answers:
// its code names the block reason — safety, prohibited_content, blocklist,
// image_safety, or content_blocked. See TheoryOfDecisions.
type DecisionsAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DecisionsErrorResponse wraps the error object. See TheoryOfDecisions.
type DecisionsErrorResponse struct {
	Error *DecisionsAPIError `json:"error,omitempty"`
}

// DecisionsError reports a failed decisions request. The request stays out of
// the error text, so phrase-based classification such as IsContextExceeded
// reads the provider's own report, never an echoed request. See
// TheoryOfDecisions.
type DecisionsError struct {
	Err     error
	Request DecisionsRequest
}

func (e DecisionsError) Error() string {
	return e.Err.Error()
}

// Unwrap exposes the transport error, so the retry classifier reaches
// the underlying network fault through the provider's wrapper. See
// TheoryOfTransientTransportErrors.
func (e DecisionsError) Unwrap() error {
	return e.Err
}

// Decisions is a Generator backed by the Gemini Decisions API: it asks one
// noul question per declared function, selects every function whose answer
// reaches the threshold, and generates no text. See TheoryOfDecisions.
type Decisions struct {
	spec   Spec
	apiKey string
	client nets.HTTPClient

	Count         dscope.Inject[BPETokenCounter]
	Logger        dscope.Inject[logs.Logger]
	EventRecorder dscope.Inject[EventRecorder]
	FuncDecls     dscope.Inject[FuncDecls]
}

var _ Generator = Decisions{}

func (d Decisions) Spec() Spec {
	return d.spec
}

// CountTokens estimates the token count with the shared BPE counter: the
// protocol publishes no tokenizer, and the exact counts of a request come
// back in its usage. See TheoryOfDecisions.
func (d Decisions) CountTokens(text string) (int, error) {
	return d.Count()(text)
}

// recordEvent records an API-level event (api_error) in the scope's
// generators.EventSink: the per-scope EventSink is the default
// EventRecorder, injected as a dscope dependency when the generator is
// constructed, and the generation loop drains the sink after each round,
// recording every buffered event as a session-tree event node. See
// generators.TheoryOfEventRecorder.
func (d Decisions) recordEvent(typ string, detail string) {
	if rec := d.EventRecorder(); rec != nil && rec.Enabled() {
		rec.Event(typ, detail)
	}
}

// NewDecisions builds a Decisions generator from a spec. The HTTP client and
// the configured Gemini API key arrive from the scope, so proxy handling and
// credentials stay uniform with the Gemini content generator. The spec's own
// API key takes precedence and falls back to the configured key. See
// TheoryOfDecisions.
type NewDecisions func(spec Spec) Decisions

func (Module) NewDecisions(
	inject dscope.InjectStruct,
	client nets.HTTPClient,
	apiKey GoogleAPIKey,
) NewDecisions {
	return func(spec Spec) Decisions {
		if spec.BaseURL == "" {
			spec.BaseURL = decisionsBaseURL
		}
		if spec.Model == "" {
			spec.Model = decisionsModel
		}
		ret := Decisions{
			spec:   spec,
			apiKey: firstNonZero(spec.APIKey, string(apiKey)),
			client: client,
		}
		inject(&ret)
		return ret
	}
}

func (d Decisions) Generate(ctx context.Context, state State, options *GenerateOptions) (ret State, err error) {
	ret = state

	var funcs []FuncDecl
	if d.spec.DisableTools == nil || !*d.spec.DisableTools {
		funcs = decisionsFunctions(ret, d.FuncDecls())
	}
	if len(funcs) == 0 {
		d.Logger().WarnContext(ctx, "decisions request without questions",
			"name", d.spec.Name,
			"model", d.spec.Model,
			"disable_tools", d.spec.DisableTools != nil && *d.spec.DisableTools,
		)
		return ret, fmt.Errorf("decisions: no questions to ask: declare the functions to evaluate in the state or in the func_decls config")
	}

	input := decisionsInput(ret)
	if len(input) == 0 {
		d.Logger().WarnContext(ctx, "decisions request without input",
			"name", d.spec.Name,
			"model", d.spec.Model,
		)
		return ret, fmt.Errorf("decisions: no input to evaluate")
	}

	questions, names := decisionsQuestions(funcs)
	request := DecisionsRequest{
		Model:     d.spec.Model,
		Input:     input,
		Questions: questions,
	}

	d.Logger().InfoContext(ctx, "decisions request built",
		"name", d.spec.Name,
		"model", d.spec.Model,
		"base_url", d.spec.BaseURL,
		"api_key_set", d.apiKey != "",
		"questions", len(request.Questions),
		"input_blocks", len(input),
	)

	response, duration, err := d.call(ctx, request)
	if err != nil {
		d.Logger().WarnContext(ctx, "decisions generate failed",
			"name", d.spec.Name,
			"model", d.spec.Model,
			"error", err,
		)
		return ret, err
	}

	if ret, err = decisionsApply(ret, response, names, duration); err != nil {
		d.Logger().WarnContext(ctx, "recording decisions response failed",
			"model", d.spec.Model,
			"error", err,
		)
		return ret, err
	}

	if ret, err = ret.Flush(); err != nil {
		d.Logger().WarnContext(ctx, "flushing decisions state failed",
			"model", d.spec.Model,
			"error", err,
		)
		return ret, err
	}

	d.Logger().InfoContext(ctx, "decisions generate done",
		"model", d.spec.Model,
		"answers", len(response.Answers),
		"content_count", CountContents(ret),
	)

	return ret, nil
}

// call sends one decisions request and returns the parsed response with the
// measured round-trip duration. A transport fault, a retryable status, and a
// rejected request all come back as a DecisionsError carrying the request, so
// no error text echoes the input. See TheoryOfDecisions.
func (d Decisions) call(ctx context.Context, request DecisionsRequest) (*DecisionsResponse, time.Duration, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, 0, err
	}

	url := strings.TrimSuffix(d.spec.BaseURL, "/") + "/decisions"

	client := d.client
	if d.spec.NoProxy != nil && *d.spec.NoProxy {
		client = nets.HTTPClient{
			Client: &http.Client{
				Transport: &http.Transport{
					DialContext: (&net.Dialer{}).DialContext,
				},
			},
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	// The key travels in the header the Decisions API documents; an endpoint
	// that needs no key gets no header. See TheoryOfDecisions.
	if d.apiKey != "" {
		httpReq.Header.Set("x-goog-api-key", d.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	d.Logger().InfoContext(ctx, "sending decisions request",
		"model", d.spec.Model,
		"url", url,
		"no_proxy", d.spec.NoProxy != nil && *d.spec.NoProxy,
		"body_bytes", len(body),
	)

	started := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		d.Logger().WarnContext(ctx, "decisions request failed",
			"model", d.spec.Model,
			"url", url,
			"error", err,
		)
		d.recordEvent("api_error", fmt.Sprintf("decisions request failed: %v", err))
		return nil, 0, DecisionsError{
			Err:     err,
			Request: request,
		}
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		d.Logger().WarnContext(ctx, "reading decisions response failed",
			"model", d.spec.Model,
			"error", err,
		)
		d.recordEvent("api_error", fmt.Sprintf("decisions response read failed: %v", err))
		return nil, 0, DecisionsError{
			Err:     err,
			Request: request,
		}
	}
	duration := time.Since(started)

	d.Logger().InfoContext(ctx, "decisions response status",
		"model", d.spec.Model,
		"status", resp.StatusCode,
		"content_type", resp.Header.Get("Content-Type"),
	)

	if resp.StatusCode != http.StatusOK {
		statusErr := DecisionsError{
			Err: fmt.Errorf("decisions http status %d: %s",
				resp.StatusCode,
				decisionsErrorMessage(responseBody),
			),
			Request: request,
		}
		d.Logger().WarnContext(ctx, "decisions http status",
			"model", d.spec.Model,
			"status", resp.StatusCode,
			"error", statusErr.Err,
		)
		d.recordEvent("api_error", statusErr.Err.Error())
		// Rate limiting and server errors are transient: join ErrRetryable
		// so the generate phase retries them. See TheoryOfDecisions.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, 0, errors.Join(statusErr, ErrRetryable)
		}
		return nil, 0, statusErr
	}

	var response DecisionsResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		d.Logger().WarnContext(ctx, "unmarshalling decisions response failed",
			"model", d.spec.Model,
			"error", err,
		)
		d.recordEvent("api_error", fmt.Sprintf("decisions response unmarshal failed: %v", err))
		return nil, 0, DecisionsError{
			Err:     err,
			Request: request,
		}
	}

	return &response, duration, nil
}

// decisionsApply records one response into the state: the usage, the function
// calls the answers select, and the full answer map as log content, in that
// order, so the transcript reads cost, act, then evidence. See
// TheoryOfDecisions.
func decisionsApply(
	state State,
	response *DecisionsResponse,
	names map[string]string,
	duration time.Duration,
) (ret State, err error) {
	ret = state

	if response.Usage != nil {
		var usage Usage
		usage.Prompt.TokenCount = response.Usage.TotalInputTokens
		usage.Candidates.TokenCount = response.Usage.TotalOutputTokens
		usage.TotalDuration = duration
		if ret, err = ret.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{usage},
		}); err != nil {
			return ret, err
		}
	}

	selected := decisionsSelectedFunctions(response.Answers)
	if len(selected) > 0 {
		content := &Content{
			Role: RoleModel,
		}
		for _, key := range selected {
			name := names[key]
			if name == "" {
				name = key
			}
			content.Parts = append(content.Parts, FuncCall{
				ID:        name,
				Name:      name,
				Arguments: map[string]any{},
			})
		}
		if ret, err = ret.AppendContent(content); err != nil {
			return ret, err
		}
	}

	answers, marshalErr := json.Marshal(response.Answers)
	if marshalErr != nil {
		return ret, marshalErr
	}
	if ret, err = ret.AppendContent(&Content{
		Role: RoleLog,
		Parts: []Part{
			Text(fmt.Sprintf("decisions answers: %s", answers)),
		},
	}); err != nil {
		return ret, err
	}

	return ret, nil
}

// decisionsErrorMessage renders the API's error object when the body holds
// one, so the report names the API's own code — a safety block — instead of a
// bare status line. See TheoryOfDecisions.
func decisionsErrorMessage(body []byte) string {
	var response DecisionsErrorResponse
	if err := json.Unmarshal(body, &response); err == nil && response.Error != nil {
		if response.Error.Code != "" {
			return response.Error.Code + ": " + response.Error.Message
		}
		return response.Error.Message
	}
	return strings.TrimSpace(string(body))
}

// decisionsInput renders the model-facing conversation as the request input:
// the system prompt first, because the protocol has no system instruction
// field, then one text block per run of text parts and one typed media block
// per file part, in conversation order. The protocol concatenates adjacent
// text blocks with no separator, so every text block ends with a blank line:
// text fragments join verbatim, and a file URL or a text file joins the input
// as its own unit, every unit separated by a blank line. See
// TheoryOfContentUnitSeparation and TheoryOfDecisions.
func decisionsInput(state State) []DecisionsContent {
	var ret []DecisionsContent
	appendText := func(units []string) {
		if len(units) == 0 {
			return
		}
		ret = append(ret, DecisionsContent{
			Type: "text",
			Text: strings.Join(units, "\n\n") + "\n\n",
		})
	}

	if system := state.SystemPrompt(); system != "" {
		appendText([]string{system})
	}

	for content := range state.Contents() {
		if content.Role == RoleLog || content.Role == RoleSystem {
			continue
		}
		var units []string
		var run []string
		flushRun := func() {
			if len(run) > 0 {
				units = append(units, strings.Join(run, ""))
				run = nil
			}
		}
		for _, part := range content.Parts {
			switch part := part.(type) {
			case Text:
				if len(part) > 0 {
					run = append(run, string(part))
				}
			case FileURL:
				if len(part) > 0 {
					flushRun()
					units = append(units, string(part))
				}
			case FileContent:
				block, isMedia := decisionsMediaBlock(part)
				if !isMedia {
					if isTextMIMEType(part.MimeType) && len(part.Content) > 0 {
						flushRun()
						units = append(units, string(part.Content))
					}
					continue
				}
				flushRun()
				appendText(units)
				units = nil
				ret = append(ret, block)
			}
		}
		flushRun()
		appendText(units)
	}

	return ret
}

// decisionsMediaBlock maps a file part to a typed media block: the block type
// follows the MIME type and the bytes travel base64-encoded. A MIME type with
// no block type, or an empty file, yields false and the caller keeps the part
// out of the media stream. See TheoryOfDecisions.
func decisionsMediaBlock(file FileContent) (DecisionsContent, bool) {
	if len(file.Content) == 0 {
		return DecisionsContent{}, false
	}
	var typ string
	switch {
	case strings.HasPrefix(file.MimeType, "image/"):
		typ = "image"
	case strings.HasPrefix(file.MimeType, "audio/"):
		typ = "audio"
	case strings.HasPrefix(file.MimeType, "video/"):
		typ = "video"
	case file.MimeType == "application/pdf":
		typ = "document"
	default:
		return DecisionsContent{}, false
	}
	return DecisionsContent{
		Type:     typ,
		Data:     base64.StdEncoding.EncodeToString(file.Content),
		MimeType: file.MimeType,
	}, true
}

// decisionsFunctions collects the declared functions: the state's own
// functions first, then the configured declarations, deduplicated by name and
// sorted by name so equal states produce equal requests. See
// TheoryOfPrefixCaching.
func decisionsFunctions(state State, decls FuncDecls) []FuncDecl {
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

// decisionsQuestions turns the declared functions into the request's
// questions and the key-to-name map that translates the answers back. See
// TheoryOfDecisions.
func decisionsQuestions(funcs []FuncDecl) (map[string]DecisionsQuestion, map[string]string) {
	questions := make(map[string]DecisionsQuestion, len(funcs))
	names := make(map[string]string, len(funcs))
	taken := make(map[string]bool, len(funcs))
	for _, fn := range funcs {
		key := decisionsQuestionKey(fn.Name, taken)
		taken[key] = true
		names[key] = fn.Name
		questions[key] = DecisionsQuestion{
			Type:         "noul",
			Instructions: decisionsFunctionInstruction(fn),
			Criteria: map[string]any{
				"true_description":  "the function handles the latest user message",
				"false_description": "another function or a plain answer handles the latest user message",
			},
		}
	}
	return questions, names
}

// decisionsQuestionKey derives a question key from a function name: the key
// alphabet is letters, digits, underscores, and dashes, so every other
// character becomes an underscore; a key already in use gains a numeric
// suffix, and a key longer than the protocol's limit is truncated. The
// derived key is unique inside taken. See TheoryOfDecisions.
func decisionsQuestionKey(name string, taken map[string]bool) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	base := b.String()
	if base == "" {
		base = "question"
	}
	if len(base) > decisionsMaxQuestionKey {
		base = base[:decisionsMaxQuestionKey]
	}
	key := base
	for i := 2; taken[key]; i++ {
		suffix := fmt.Sprintf("-%d", i)
		trimmed := base
		if len(trimmed) > decisionsMaxQuestionKey-len(suffix) {
			trimmed = trimmed[:decisionsMaxQuestionKey-len(suffix)]
		}
		key = trimmed + suffix
	}
	return key
}

// decisionsFunctionInstruction writes the function identity into the
// instruction text: the protocol reads text instructions only and never sees
// question keys or the caller's variable names, so the name and description
// must travel in the question itself. See TheoryOfDecisions.
func decisionsFunctionInstruction(fn FuncDecl) string {
	var b strings.Builder
	b.WriteString("Does the function ")
	b.WriteString(fn.Name)
	if fn.Description != "" {
		b.WriteString(" (")
		b.WriteString(fn.Description)
		b.WriteString(")")
	}
	b.WriteString(" handle the latest user message?")
	return b.String()
}

// decisionsSelectedFunctions returns the question keys whose noul answer
// reaches the selection threshold, ordered by descending probability and then
// by key, so equal answers produce equal output. See TheoryOfDecisions.
func decisionsSelectedFunctions(answers map[string]DecisionsAnswer) []string {
	type selection struct {
		key  string
		noul float64
	}
	var selections []selection
	for key, answer := range answers {
		if answer.Type != "noul" || answer.Noul == nil {
			continue
		}
		if *answer.Noul < decisionsSelectionThreshold {
			continue
		}
		selections = append(selections, selection{
			key:  key,
			noul: *answer.Noul,
		})
	}
	sort.Slice(selections, func(i, j int) bool {
		if selections[i].noul != selections[j].noul {
			return selections[i].noul > selections[j].noul
		}
		return selections[i].key < selections[j].key
	})
	var ret []string
	for _, selection := range selections {
		ret = append(ret, selection.key)
	}
	return ret
}
