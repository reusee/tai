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
	"net/http"
	"strings"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/vars"
)

type OpenAI struct {
	args   GeneratorArgs
	apiKey string
	client nets.HTTPClient

	Count  dscope.Inject[BPETokenCounter]
	Logger dscope.Inject[logs.Logger]
	Tap    dscope.Inject[debugs.Tap]
	Loader dscope.Inject[configs.Loader]
}

var _ Generator = new(OpenAI)

func (o *OpenAI) Args() GeneratorArgs {
	return o.args
}

func (o *OpenAI) CountTokens(text string) (int, error) {
	return o.Count()(text)
}

func (o *OpenAI) Generate(ctx context.Context, state State) (ret State, err error) {
	ret = state

	messages, err := stateToOpenAIMessages(ret)
	if err != nil {
		return nil, err
	}

	var tools []Tool

	for _, fn := range ret.FuncMap() {
		tools = append(tools, fn.Decl.ToOpenAI())
	}
	for set := range configs.All[[]FuncDecl](o.Loader(), "functions") {
		for _, fn := range set {
			tools = append(tools, fn.ToOpenAI())
		}
	}

	temperature := float32(0)
	if o.args.Temperature != nil {
		temperature = *o.args.Temperature
	}
	if *temperatureFlag != 0 {
		temperature = *temperatureFlag
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
			"args":     o.args,
			"tools":    tools,
		})
	}

	o.Logger().InfoContext(ctx, "generating",
		"model", o.args.Model,
	)

	req := ChatCompletionRequest{
		Model:               o.args.Model,
		Messages:            messages,
		Stream:              true,
		ReasoningEffort:     "high",
		MaxCompletionTokens: vars.DerefOrZero(o.args.MaxGenerateTokens),
		Temperature:         temperature,
		//TODO o.args.ExtraArguments
	}

	if !o.args.DisableTools {
		req.Tools = tools
	}

	if o.args.IsOpenRouter {
		req.Reasoning = new(Reasoning)
		req.Reasoning.Effort = req.ReasoningEffort
		req.ReasoningEffort = ""
	}

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", o.args.BaseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := o.client.Do(httpReq)
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
		if err := json.Unmarshal(body, &errResp); err != nil {
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

		//TODO fix
		//if streamResp.Usage != nil {
		//	var usage Usage
		//	usage.Prompt.TokenCount = streamResp.Usage.PromptTokens
		//	if streamResp.Usage.PromptTokensDetails != nil {
		//		usage.Prompt.TokenCountCached = streamResp.Usage.PromptTokensDetails.CachedTokens
		//	}
		//	usage.Candidates.TokenCount = streamResp.Usage.CompletionTokens
		//	if streamResp.Usage.CompletionTokensDetails != nil {
		//		usage.Candidates.TokenCount -= streamResp.Usage.CompletionTokensDetails.ReasoningTokens
		//		usage.Thoughts.TokenCount = streamResp.Usage.CompletionTokensDetails.ReasoningTokens
		//	}
		//	if ret, err = ret.AppendContent(&Content{
		//		Role:  RoleLog,
		//		Parts: []Part{usage},
		//	}); err != nil {
		//		return ret, err
		//	}
		//}

		if len(streamResp.Choices) == 0 {
			o.Logger().Info("no choices")
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

	addPart := func(role string, part ChatMessagePart) {
		if role == string(RoleModel) {
			// convert to open ai role
			role = string(RoleAssistant)
		}
		if len(messages) == 0 {
			messages = append(messages, ChatCompletionMessage{
				Role: role,
			})
		}
		last := messages[len(messages)-1]
		if last.Role != role {
			messages = append(messages, ChatCompletionMessage{
				Role: role,
			})
			last = messages[len(messages)-1]
		}
		last.MultiContent = append(last.MultiContent, part)
		messages[len(messages)-1] = last
	}

	if contents := state.Contents(); len(contents) > 0 {
		for _, content := range contents {

			for _, part := range content.Parts {
				switch part := part.(type) {

				case Text:
					if len(part) > 0 {
						addPart(string(content.Role), ChatMessagePart{
							Type: "text",
							Text: string(part),
						})
					}

				case Thought:
					if len(part) > 0 {
						// Thoughts are handled as text parts with special tags
						addPart(string(content.Role), ChatMessagePart{
							Type: "text",
							Text: "<thought>" + string(part) + "</thought>",
						})
					}

				case FileURL:
					if len(part) > 0 {
						addPart(string(content.Role), ChatMessagePart{
							Type: "image_url",
							ImageURL: &ChatMessageImageURL{
								URL: string(part),
							},
						})
					}

				case FileContent:
					if isTextMIMEType(part.MimeType) {
						addPart(string(content.Role), ChatMessagePart{
							Type: "text",
							Text: string(part.Content),
						})
					} else {
						dataURL := fmt.Sprintf("data:%s;base64,%s",
							part.MimeType,
							base64.StdEncoding.EncodeToString(part.Content),
						)
						addPart(string(content.Role), ChatMessagePart{
							Type: "image_url",
							ImageURL: &ChatMessageImageURL{
								URL: dataURL,
							},
						})
					}

				case FuncCall:
					argsBytes, err := json.Marshal(part.Args)
					if err != nil {
						return nil, err
					}
					messages = append(messages, ChatCompletionMessage{
						Role: "assistant",
						ToolCalls: []ToolCall{
							{
								ID:   part.ID,
								Type: "function",
								Function: FunctionCall{
									Name:      part.Name,
									Arguments: string(argsBytes),
								},
							},
						},
					})

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
	}

	// convert single text part MultiContent to Content
	for i, msg := range messages {
		if len(msg.ToolCalls) > 0 {
			continue
		}
		if len(msg.Content) > 0 {
			continue
		}
		if len(msg.MultiContent) != 1 {
			continue
		}
		part := msg.MultiContent[0]
		if part.Type != "text" {
			continue
		}
		messages[i].Content = part.Text
		messages[i].MultiContent = nil
	}

	return
}

type NewOpenAI func(args GeneratorArgs, apiKey string) *OpenAI

func (Module) NewOpenAI(
	inject dscope.InjectStruct,
	client nets.HTTPClient,
) NewOpenAI {
	return func(args GeneratorArgs, apiKey string) *OpenAI {
		ret := &OpenAI{
			args:   args,
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
	ReasoningEffort     string                  `json:"reasoning_effort,omitempty"`
	Reasoning           *Reasoning              `json:"reasoning,omitempty"`
	MaxCompletionTokens int                     `json:"max_completion_tokens,omitempty"`
	Temperature         float32                 `json:"temperature,omitempty"`
	Tools               []Tool                  `json:"tools,omitempty"`
}

type Reasoning struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Exclude   bool   `json:"exclude,omitempty"`
	Enabled   bool   `json:"enabled,omitempty"`
}

type ChatCompletionMessage struct {
	Role         string            `json:"role"`
	Content      string            `json:"content,omitempty"`
	MultiContent []ChatMessagePart `json:"multi_content,omitempty"`
	ToolCalls    []ToolCall        `json:"tool_calls,omitempty"`
	ToolCallID   string            `json:"tool_call_id,omitempty"`
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
	//TODO Usage
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
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
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

func (e *APIError) Error() string {
	return e.Message
}
