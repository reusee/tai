package generators

import (
	"bufio"
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

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
)

type OpenAI struct {
	spec   Spec
	apiKey string
	client nets.HTTPClient

	Count                dscope.Inject[BPETokenCounter]
	TokenCounterOverride TokenCounter
	Logger               dscope.Inject[logs.Logger]
	Tap                  dscope.Inject[debugs.Tap]
	Loader               dscope.Inject[configs.Loader]
	Effort               dscope.Inject[EffortFlag]
}

var _ Generator = new(OpenAI)

func (o *OpenAI) Spec() Spec {
	return o.spec
}

func (o *OpenAI) CountTokens(text string) (int, error) {
	if o.TokenCounterOverride != nil {
		return o.TokenCounterOverride(text)
	}
	return o.Count()(text)
}

func (o *OpenAI) Generate(ctx context.Context, state State, options *GenerateOptions) (ret State, err error) {
	ret = state

	messages, err := stateToOpenAIMessages(ret)
	if err != nil {
		return nil, err
	}

	var tools []Tool

	if o.spec.DisableTools == nil || !*o.spec.DisableTools {
		// Collect all function declarations from state and config into a single
		// slice, then sort globally by name. Global sorting maximizes prefix
		// cache reuse: adding a function from any source inserts it at its
		// natural alphabetical position, shifting only the functions that follow.
		// See TheoryOfPrefixCaching for rationale.
		var allFuncs []FuncDecl
		for fn := range ret.Functions() {
			allFuncs = append(allFuncs, fn.Decl)
		}
		for set := range configs.All[[]FuncDecl](o.Loader(), "functions") {
			allFuncs = append(allFuncs, set...)
		}
		sort.SliceStable(allFuncs, func(i, j int) bool {
			return allFuncs[i].Name < allFuncs[j].Name
		})
		for _, fn := range allFuncs {
			tools = append(tools, fn.ToOpenAI())
		}
	}

	temperature := float32(0)
	if o.spec.Temperature != nil {
		temperature = *o.spec.Temperature
	}
	if *temperatureFlag != 0 {
		temperature = float32(*temperatureFlag)
	}

	if *debugOpenAI {
		jsonText, err := json.Marshal(messages)
		if err != nil {
			return nil, err
		}
		o.Logger().InfoContext(ctx, "open ai messages to send",
			"messages", jsonText,
		)
	}

	if *tapOpenAI {
		o.Tap()(ctx, "before CreateChatCompletionStream", map[string]any{
			"messages": messages,
			"spec":     o.spec,
			"tools":    tools,
		})
	}

	nonStreaming := false
	if options != nil && options.NonStreaming {
		nonStreaming = true
	}

	o.Logger().InfoContext(ctx, "generating",
		"model", o.spec.Model,
		"non_streaming", nonStreaming,
	)

	maxCompletionTokens := 0
	if o.spec.MaxGenerateTokens != nil {
		maxCompletionTokens = *o.spec.MaxGenerateTokens
	}
	if options != nil && options.MaxGenerateTokens != nil {
		n := *options.MaxGenerateTokens
		if maxCompletionTokens == 0 || n < maxCompletionTokens {
			maxCompletionTokens = n
		}
	}

	req := ChatCompletionRequest{
		Model:               o.spec.Model,
		Messages:            messages,
		Stream:              !nonStreaming,
		MaxCompletionTokens: maxCompletionTokens,
		Temperature:         temperature,
	}
	reasoningEffort := o.spec.ReasoningEffort
	if flagEffort := string(o.Effort()); flagEffort != "" {
		reasoningEffort = flagEffort
	}
	if reasoningEffort != "" {
		req.ReasoningEffort = reasoningEffort
	}

	if !nonStreaming {
		req.StreamOptions = &StreamOptions{IncludeUsage: true}
	}

	if o.spec.DisableTools == nil || !*o.spec.DisableTools {
		req.Tools = tools
	}

	if o.spec.IsOpenRouter != nil && *o.spec.IsOpenRouter && (req.ReasoningEffort != "" || o.spec.MaxThinkingTokens != nil) {
		req.Reasoning = &Reasoning{}
		if req.ReasoningEffort != "" {
			req.Reasoning.Effort = req.ReasoningEffort
			req.ReasoningEffort = ""
		}
		if o.spec.MaxThinkingTokens != nil {
			req.Reasoning.MaxTokens = *o.spec.MaxThinkingTokens
		}
	}

	if options != nil && options.ResponseSchema != nil {
		req.ResponseFormat = &ResponseFormat{
			Type: "json_schema",
			JSONSchema: &JSONSchema{
				Name:   "response",
				Strict: true,
				Schema: options.ResponseSchema.ToOpenAI(),
			},
		}
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := o.spec.BaseURL + "/chat/completions"
	if o.spec.IsAzure != nil && *o.spec.IsAzure {
		url = fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
			strings.TrimSuffix(o.spec.BaseURL, "/"),
			o.spec.Model,
			o.spec.APIVersion,
		)
	}

	// Select HTTP client based on NoProxy flag
	client := o.client
	if o.spec.NoProxy != nil && *o.spec.NoProxy {
		client = nets.HTTPClient{
			Client: &http.Client{
				Transport: &http.Transport{
					DialContext: (&net.Dialer{}).DialContext,
				},
			},
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	if o.spec.IsAzure != nil && *o.spec.IsAzure {
		httpReq.Header.Set("api-key", o.apiKey)
	} else {
		httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if !nonStreaming {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return ret, OpenAIError{
			Err:     err,
			Request: req,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		// Check both unmarshal failure and nil Error field: some providers
		// return valid JSON without an "error" field (e.g. {"message": "..."}),
		// which would leave errResp.Error nil and cause a panic on the next
		// line when setting HTTPStatusCode.
		if err := json.Unmarshal(body, &errResp); err != nil || errResp.Error == nil {
			err := fmt.Errorf("bad status: %d, body: %s", resp.StatusCode, string(body))
			if resp.StatusCode == http.StatusTooManyRequests {
				return ret, errors.Join(err, ErrRetryable)
			}
			return ret, OpenAIError{
				Err:     err,
				Request: req,
			}
		}

		errResp.Error.HTTPStatusCode = resp.StatusCode
		if resp.StatusCode == http.StatusTooManyRequests {
			return ret, errors.Join(errResp.Error, ErrRetryable)
		}
		return ret, OpenAIError{
			Err:     errResp.Error,
			Request: req,
		}
	}

	handleUsage := func(u *OpenAIUsage) error {
		if u == nil {
			return nil
		}
		var usage Usage
		usage.Prompt.TokenCount = u.PromptTokens
		if u.PromptTokensDetails != nil {
			usage.Prompt.TokenCountCached = u.PromptTokensDetails.CachedTokens
		}
		usage.Candidates.TokenCount = u.CompletionTokens
		if u.CompletionTokensDetails != nil {
			usage.Candidates.TokenCount -= u.CompletionTokensDetails.ReasoningTokens
			usage.Thoughts.TokenCount = u.CompletionTokensDetails.ReasoningTokens
		}
		var err error
		if ret, err = ret.AppendContent(&Content{
			Role:  RoleLog,
			Parts: []Part{usage},
		}); err != nil {
			return err
		}
		return nil
	}

	if nonStreaming {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return ret, err
		}
		if *debugOpenAI {
			o.Logger().InfoContext(ctx, "OpenAI response",
				"body", string(body),
			)
		}
		var response ChatCompletionResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return ret, err
		}

		if err := handleUsage(response.Usage); err != nil {
			return ret, err
		}

		if len(response.Choices) > 0 {
			choice := response.Choices[0]
			msg := choice.Message
			role := Role(msg.Role)
			if role == RoleAssistant {
				role = RoleModel
			}
			content := &Content{
				Role: role,
			}
			reasoningContent := msg.ReasoningContent
			if reasoningContent == "" {
				reasoningContent = msg.Reasoning
			}
			if reasoningContent != "" {
				content.Parts = append(content.Parts, Thought(reasoningContent))
			}
			if contentStr, ok := msg.Content.(string); ok && contentStr != "" {
				content.Parts = append(content.Parts, Text(contentStr))
			}
			for _, call := range msg.ToolCalls {
				var arguments map[string]any
				if call.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Function.Arguments), &arguments); err != nil {
						return ret, err
					}
				}
				content.Parts = append(content.Parts, FuncCall{
					ID:        call.ID,
					Name:      call.Function.Name,
					Arguments: arguments,
				})
			}
			if ret, err = ret.AppendContent(content); err != nil {
				return ret, err
			}

			if choice.FinishReason != "" {
				if ret, err = ret.AppendContent(&Content{
					Role: RoleLog,
					Parts: []Part{
						FinishReason(choice.FinishReason),
					},
				}); err != nil {
					return ret, err
				}
				if choice.FinishReason == "error" {
					return ret, errors.Join(errors.New(string(choice.FinishReason)), ErrRetryable)
				}
			}
		}

	} else {
		parser := new(OpenAIParser)
		finish := func() error {
			if contents, err := parser.End(); err != nil {
				return err
			} else {
				for _, content := range contents {
					if *debugOpenAI {
						o.Logger().InfoContext(ctx, "OpenAI content",
							"details", content,
						)
					}
					if ret, err = ret.AppendContent(content); err != nil {
						return err
					}
				}
			}
			return nil
		}

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(nil, 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			if strings.HasPrefix(line, "data: [DONE]") {
				break
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := line[6:]

			var streamResp ChatCompletionStreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				return ret, fmt.Errorf("error unmarshalling stream response: %w", err)
			}

			if *debugOpenAI {
				o.Logger().InfoContext(ctx, "OpenAI response",
					"details", streamResp,
				)
			}

			if err := handleUsage(streamResp.Usage); err != nil {
				return ret, err
			}

			if len(streamResp.Choices) == 0 {
				continue
			}

			newContents, err := parser.Input(streamResp.Choices[0].Delta)
			if err != nil {
				return ret, err
			}

			for _, content := range newContents {
				if *debugOpenAI {
					o.Logger().InfoContext(ctx, "OpenAI content",
						"details", content,
					)
				}
				// Return ret to preserve partial state accumulated during
				// streaming. Returning nil would discard content from
				// previous successful AppendContent calls, losing all
				// streamed content received before the error.
				if ret, err = ret.AppendContent(content); err != nil {
					return ret, err
				}
			}

			if reason := streamResp.Choices[0].FinishReason; reason != "" {
				if err := finish(); err != nil {
					return ret, err
				}
				if ret, err = ret.AppendContent(&Content{
					Role: RoleLog,
					Parts: []Part{
						FinishReason(reason),
					},
				}); err != nil {
					return ret, err
				}
				if reason == "error" {
					return ret, errors.Join(errors.New(string(reason)), ErrRetryable)
				}
			}

		}
		if err := scanner.Err(); err != nil {
			return ret, fmt.Errorf("error reading stream: %w", err)
		}

		if err := finish(); err != nil {
			return ret, err
		}
	}

	if ret, err = ret.Flush(); err != nil {
		return ret, err
	}

	return ret, nil
}

func stateToOpenAIMessages(state State) (messages []ChatCompletionMessage, err error) {
	if state.SystemPrompt() != "" {
		messages = append(messages, ChatCompletionMessage{
			Role:    string(RoleSystem),
			Content: state.SystemPrompt(),
		})
	}

	addText := func(role, text string) {
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			last := &messages[len(messages)-1]
			switch c := last.Content.(type) {
			case string:
				last.Content = c + text
			case []ChatMessagePart:
				last.Content = append(c, ChatMessagePart{
					Type: "text",
					Text: text,
				})
			case nil:
				last.Content = text
			default:
				panic("unexpected content type")
			}
		} else {
			messages = append(messages, ChatCompletionMessage{
				Role:    role,
				Content: text,
			})
		}
	}

	addMultiContentPart := func(role string, part ChatMessagePart) {
		if len(messages) > 0 && messages[len(messages)-1].Role == role && len(messages[len(messages)-1].ToolCalls) == 0 {
			last := &messages[len(messages)-1]
			switch c := last.Content.(type) {
			case string:
				last.Content = []ChatMessagePart{
					{Type: "text", Text: c},
					part,
				}
			case []ChatMessagePart:
				last.Content = append(c, part)
			case nil:
				last.Content = []ChatMessagePart{part}
			default:
				panic("unexpected content type")
			}
		} else {
			messages = append(messages, ChatCompletionMessage{
				Role:    role,
				Content: []ChatMessagePart{part},
			})
		}
	}

	for content := range state.Contents() {
		// Skip log and system content to prevent internal metadata (Usage,
		// FinishReason, Error) from being sent to the API. This also preserves
		// prefix cache stability: log messages interspersed with conversation
		// messages would shift the position of cached content. The Gemini path
		// already filters these roles; this brings the OpenAI path to parity.
		if content.Role == RoleLog || content.Role == RoleSystem {
			continue
		}
		role := string(content.Role)
		if role == string(RoleModel) {
			role = string(RoleAssistant)
		}
		for _, part := range content.Parts {
			switch part := part.(type) {
			case Text:
				if len(part) == 0 {
					continue
				}
				addText(role, string(part))
			case Thought:
				if role == string(RoleAssistant) {
					if len(messages) > 0 && messages[len(messages)-1].Role == role {
						messages[len(messages)-1].ReasoningContent += string(part)
					} else {
						messages = append(messages, ChatCompletionMessage{
							Role:             role,
							ReasoningContent: string(part),
						})
					}
				}
			case FileURL:
				if len(part) == 0 {
					continue
				}
				imgPart := ChatMessagePart{
					Type: "image_url",
					ImageURL: &ChatMessageImageURL{
						URL: string(part),
					},
				}
				addMultiContentPart(role, imgPart)
			case FileContent:
				if isTextMIMEType(part.MimeType) {
					addText(role, string(part.Content))
					continue
				}
				dataURL := fmt.Sprintf("data:%s;base64,%s",
					part.MimeType,
					base64.StdEncoding.EncodeToString(part.Content),
				)
				msgPart := ChatMessagePart{
					Type: "image_url",
					ImageURL: &ChatMessageImageURL{
						URL: dataURL,
					},
				}
				addMultiContentPart(role, msgPart)
			case FuncCall:
				argsBytes, err := json.Marshal(part.Arguments)
				if err != nil {
					return nil, err
				}
				toolCall := ToolCall{
					ID:   part.ID,
					Type: "function",
					Function: FunctionCall{
						Name:      part.Name,
						Arguments: string(argsBytes),
					},
				}
				if len(messages) > 0 && messages[len(messages)-1].Role == "assistant" {
					messages[len(messages)-1].ToolCalls = append(messages[len(messages)-1].ToolCalls, toolCall)
				} else {
					messages = append(messages, ChatCompletionMessage{
						Role:      "assistant",
						ToolCalls: []ToolCall{toolCall},
					})
				}
			case CallResult:
				resultsBytes, err := json.Marshal(part.Results)
				if err != nil {
					return nil, err
				}
				messages = append(messages, ChatCompletionMessage{
					Role:       "tool",
					ToolCallID: part.ID,
					Content:    string(resultsBytes),
				})
			}
		}
	}

	return
}

type NewOpenAI func(spec Spec, apiKey string) *OpenAI

func (Module) NewOpenAI(
	inject dscope.InjectStruct,
	client nets.HTTPClient,
) NewOpenAI {
	return func(spec Spec, apiKey string) *OpenAI {
		ret := &OpenAI{
			spec:   spec,
			client: client,
			apiKey: apiKey,
		}
		inject(&ret)
		return ret
	}
}

type ChatCompletionRequest struct {
	Model               string                  `json:"model"`
	Messages            []ChatCompletionMessage `json:"messages"`
	Stream              bool                    `json:"stream"`
	StreamOptions       *StreamOptions          `json:"stream_options,omitempty"`
	ReasoningEffort     string                  `json:"reasoning_effort,omitempty"`
	Reasoning           *Reasoning              `json:"reasoning,omitempty"`
	MaxCompletionTokens int                     `json:"max_completion_tokens,omitempty"`
	Temperature         float32                 `json:"temperature"`
	Tools               []Tool                  `json:"tools,omitempty"`
	ResponseFormat      *ResponseFormat         `json:"response_format,omitempty"`
}

type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

type JSONSchema struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
	Schema      any    `json:"schema"`
}

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type Reasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
}

type ChatCompletionMessage struct {
	Role             string     `json:"role"`
	Content          any        `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Reasoning        string     `json:"reasoning,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
}

type ChatMessagePart struct {
	Type     string               `json:"type"`
	Text     string               `json:"text,omitempty"`
	ImageURL *ChatMessageImageURL `json:"image_url,omitempty"`
}

type ChatMessageImageURL struct {
	URL string `json:"url"`
}

type Tool struct {
	Type     string              `json:"type"`
	Function *FunctionDefinition `json:"function,omitempty"`
}

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Strict      bool   `json:"strict,omitempty"`
	Parameters  any    `json:"parameters"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChatCompletionStreamResponse struct {
	Choices []ChatCompletionStreamChoice `json:"choices"`
	Usage   *OpenAIUsage                 `json:"usage,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *OpenAIUsage           `json:"usage,omitempty"`
}

type ChatCompletionChoice struct {
	Message      ChatCompletionMessage `json:"message"`
	FinishReason string                `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
}

type ChatCompletionStreamChoice struct {
	Delta        ChatCompletionStreamChoiceDelta `json:"delta"`
	FinishReason string                          `json:"finish_reason"`
}

type ChatCompletionStreamChoiceDelta struct {
	Content          string     `json:"content,omitempty"`
	Role             string     `json:"role,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	Reasoning        string     `json:"reasoning,omitempty"`
}

type ErrorResponse struct {
	Error *APIError `json:"error,omitempty"`
}

type APIError struct {
	Code           any     `json:"code,omitempty"`
	Message        string  `json:"message,omitempty"`
	Param          *string `json:"param,omitempty"`
	Type           string  `json:"type,omitempty"`
	HTTPStatusCode int     `json:"-"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

func (e *APIError) Error() string {
	return e.Message
}
