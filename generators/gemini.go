package generators

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/vars"
	"google.golang.org/genai"
)

type Gemini struct {
	args      GeneratorArgs
	GetClient dscope.Inject[GetGeminiClient]
	Counter   dscope.Inject[GeminiTokenCounter]
	Logger    dscope.Inject[logs.Logger]
	Loader    dscope.Inject[configs.Loader]
}

var _ Generator = Gemini{}

func (g Gemini) Args() GeneratorArgs {
	return g.args
}

func (g Gemini) CountTokens(text string) (int, error) {
	return g.Counter()(g.args.Model)(text)
}

func (g Gemini) Generate(ctx context.Context, state State, options *GenerateOptions) (ret State, err error) {
	client, err := g.GetClient()(ctx, g.args.APIKey)
	if err != nil {
		return ret, err
	}

	ret = state

	var maxOutputTokens int32
	if g.args.MaxGenerateTokens != nil {
		max := int32(*g.args.MaxGenerateTokens)
		maxOutputTokens = max
	}
	if options != nil && options.MaxGenerateTokens != nil {
		n := int32(*options.MaxGenerateTokens)
		if maxOutputTokens == 0 || n < maxOutputTokens {
			maxOutputTokens = n
		}
	}

	var maxThinkingTokens *int32
	if maxOutputTokens != 0 {
		maxThinking := maxOutputTokens / 4
		maxThinkingTokens = &maxThinking
	}

	var tools []*genai.Tool
	if !g.args.DisableSearch && len(ret.FuncMap()) == 0 {
		tools = append(tools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	var funcDecls []*genai.FunctionDeclaration
	for _, fn := range ret.FuncMap() {
		funcDecls = append(funcDecls, fn.Decl.ToGemini())
	}
	for set := range configs.All[[]FuncDecl](g.Loader(), "functions") {
		for _, fn := range set {
			funcDecls = append(funcDecls, fn.ToGemini())
		}
	}
	var toolConfig *genai.ToolConfig
	if len(funcDecls) > 0 {
		tools = append(tools, &genai.Tool{
			FunctionDeclarations: funcDecls,
		})
		toolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAny,
			},
		}
	}

	safetySettings := []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockThresholdBlockNone,
		},
	}

	var contents []*genai.Content
	for _, content := range ret.Contents() {
		role := string(content.Role)
		if role == string(RoleAssistant) {
			role = string(RoleModel)
		}
		pbContent := &genai.Content{
			Role: role,
		}
		for _, part := range content.Parts {
			pbPart, err := part.ToGemini()
			if err != nil {
				return ret, err
			}
			if pbPart != nil {
				pbContent.Parts = append(pbContent.Parts, pbPart)
			}
		}
		if len(pbContent.Parts) > 0 {
			contents = append(contents, pbContent)
		}
	}

	temperature := float32(0)
	if g.args.Temperature != nil {
		temperature = float32(*g.args.Temperature)
	}
	if *temperatureFlag != 0 {
		temperature = float32(*temperatureFlag)
	}

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     &temperature,
		ThinkingConfig: &genai.ThinkingConfig{
			IncludeThoughts: true,
			ThinkingBudget:  maxThinkingTokens,
		},
		SafetySettings: safetySettings,
		Tools:          tools,
		ToolConfig:     toolConfig,
		ServiceTier:    genai.ServiceTier(g.args.ServiceTier),
	}
	if sysPrompt := ret.SystemPrompt(); sysPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{
				{Text: sysPrompt},
			},
		}
	}

	if options != nil && options.ResponseSchema != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseSchema = options.ResponseSchema.ToGemini()
	}

	nonStreaming := false
	if options != nil && options.NonStreaming {
		nonStreaming = true
	}

	ret, err = doWithRetry(ctx, g.Logger(), func() (State, error) {

		g.Logger().InfoContext(ctx, "generating",
			"model", g.args.Model,
			"non_streaming", nonStreaming,
		)

		newState := ret
		hasContent := false

		handleResponse := func(resp *genai.GenerateContentResponse) error {
			if *debugGemini {
				g.Logger().InfoContext(ctx, "gemini response",
					"details", resp,
				)
			}

			if metadata := resp.UsageMetadata; metadata != nil {
				var usage Usage
				usage.Prompt.TokenCount = int(metadata.PromptTokenCount)
				usage.Prompt.TokenCountCached = int(metadata.CachedContentTokenCount)
				usage.Candidates.TokenCount = int(metadata.CandidatesTokenCount)
				usage.Thoughts.TokenCount = int(metadata.ThoughtsTokenCount)
				var err error
				newState, err = newState.AppendContent(&Content{
					Role:  RoleLog,
					Parts: []Part{usage},
				})
				if err != nil {
					return err
				}
			}

			if len(resp.Candidates) == 0 {
				return nil
			}
			candidate := resp.Candidates[0]

			if candidate.Content != nil {
				newContent := &Content{
					Role: Role(candidate.Content.Role),
				}
				for _, part := range candidate.Content.Parts {
					if p, err := PartFromGemini(part); err != nil {
						return err
					} else if p != nil {
						if _, isThought := p.(Thought); !isThought {
							hasContent = true
						}
						newContent.Parts = append(newContent.Parts, p)
					}
				}
				var err error
				if newState, err = newState.AppendContent(newContent); err != nil {
					return err
				}
			}

			if reason := candidate.FinishReason; reason != "" {
				var err error
				if newState, err = newState.AppendContent(&Content{
					Role: RoleLog,
					Parts: []Part{
						FinishReason(string(reason)),
					},
				}); err != nil {
					return err
				}
			}
			return nil
		}

		if nonStreaming {
			resp, err := client.Models.GenerateContent(ctx, g.args.Model, contents, config)
			if err != nil {
				return ret, wrap(err)
			}
			if err := handleResponse(resp); err != nil {
				return ret, err
			}

		} else {
			for msg, err := range client.Models.GenerateContentStream(ctx, g.args.Model, contents, config) {
				if err != nil {
					if errors.Is(err, io.EOF) {
						break
					}
					return ret, wrap(err)
				}
				if err := handleResponse(msg); err != nil {
					return ret, err
				}
			}
		}

		if !hasContent {
			// no output
			return ret, errors.Join(fmt.Errorf("no output"), ErrRetryable)
		}

		return newState, nil
	})
	if err != nil {
		return ret, err
	}

	if ret, err = ret.Flush(); err != nil {
		return ret, err
	}

	return ret, nil
}

func doWithRetry[T any](
	ctx context.Context,
	logger logs.Logger,
	fn func() (T, error),
) (ret T, err error) {
	const maxRetries = 10
	backoff := 1 * time.Second

	for i := range maxRetries {
		ret, err = fn()
		if err == nil {
			return
		}
		if isRetryable(err) {
			logger.WarnContext(ctx, "retry",
				"attempt", i+1, "error", err,
			)
			select {
			case <-ctx.Done():
				err = ctx.Err()
				return
			case <-time.After(backoff * time.Duration(1<<i)):
			}
			continue
		}
		return ret, err
	}

	return
}

func isRetryable(err error) bool {
	if errors.Is(err, ErrRetryable) {
		return true
	}
	if apiErr, ok := errors.AsType[*genai.APIError](err); ok {
		if apiErr.Code == 429 || apiErr.Code == 503 || apiErr.Code == 500 {
			return true
		}
	}
	return false
}

type GetGeminiClient = func(ctx context.Context, key string) (*genai.Client, error)

func (Module) GetGeminiClient(
	httpClient nets.HTTPClient,
	apiKey GoogleAPIKey,
) GetGeminiClient {
	var clients sync.Map // key -> *genai.Client
	return func(ctx context.Context, key string) (*genai.Client, error) {
		key = vars.FirstNonZero(
			key,
			string(apiKey),
		)

		if v, ok := clients.Load(key); ok {
			return v.(*genai.Client), nil
		}

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     key,
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: httpClient,
		})
		if err != nil {
			return nil, err
		}

		v, _ := clients.LoadOrStore(key, client)
		return v.(*genai.Client), nil
	}
}

type NewGemini func(args GeneratorArgs) Gemini

func (Module) NewGemini(
	inject dscope.InjectStruct,
) NewGemini {
	return func(args GeneratorArgs) Gemini {
		ret := Gemini{
			args: args,
		}
		inject(&ret)
		return ret
	}
}
