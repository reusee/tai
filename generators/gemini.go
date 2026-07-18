package generators

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/vars"
	"google.golang.org/genai"
)

func isTerminalFinishReason(reason genai.FinishReason) bool {
	switch reason {
	case genai.FinishReasonSafety,
		genai.FinishReasonRecitation,
		genai.FinishReasonBlocklist,
		genai.FinishReasonProhibitedContent,
		genai.FinishReasonSPII,
		genai.FinishReasonMalformedFunctionCall:
		return true
	}
	return false
}

type Gemini struct {
	spec      Spec
	GetClient dscope.Inject[GetGeminiClient]
	APIKey    dscope.Inject[GoogleAPIKey]
	Counter   dscope.Inject[GeminiTokenCounter]
	Logger    dscope.Inject[logs.Logger]
	Loader    dscope.Inject[configs.Loader]
	Effort    dscope.Inject[EffortFlag]
}

var _ Generator = Gemini{}

func (g Gemini) Spec() Spec {
	return g.spec
}

func (g Gemini) CountTokens(text string) (int, error) {
	return g.Counter()(g.spec.Model)(text)
}

func (g Gemini) Generate(ctx context.Context, state State, options *GenerateOptions) (ret State, err error) {
	var client *genai.Client
	if g.spec.NoProxy != nil && *g.spec.NoProxy {
		key := vars.FirstNonZero(
			g.spec.APIKey,
			string(g.APIKey()),
		)
		directClient := &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{}).DialContext,
			},
		}
		client, err = genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     key,
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: directClient,
		})
		if err != nil {
			return ret, err
		}
	} else {
		client, err = g.GetClient()(ctx, g.spec.APIKey)
		if err != nil {
			return ret, err
		}
	}

	ret = state

	var maxOutputTokens int32
	if g.spec.MaxGenerateTokens != nil {
		max := int32(*g.spec.MaxGenerateTokens)
		maxOutputTokens = max
	}
	if options != nil && options.MaxGenerateTokens != nil {
		n := int32(*options.MaxGenerateTokens)
		if maxOutputTokens == 0 || n < maxOutputTokens {
			maxOutputTokens = n
		}
	}

	thinkingConfig := &genai.ThinkingConfig{
		IncludeThoughts: true,
	}
	if g.spec.MaxThinkingTokens != nil {
		// Explicit thinking token budget takes precedence over effort level
		// and the fallback computation from max output tokens.
		budget := int32(*g.spec.MaxThinkingTokens)
		thinkingConfig.ThinkingBudget = &budget
	} else {
		reasoningEffort := g.spec.ReasoningEffort
		if flagEffort := string(g.Effort()); flagEffort != "" {
			reasoningEffort = flagEffort
		}
		if reasoningEffort != "" {
			thinkingConfig.ThinkingLevel = genai.ThinkingLevel(reasoningEffort)
		} else {
			// set budget from max output tokens
			var maxThinkingTokens *int32
			if maxOutputTokens != 0 {
				maxThinking := maxOutputTokens / 4
				maxThinkingTokens = &maxThinking
			}
			if maxThinkingTokens != nil {
				thinkingConfig.ThinkingBudget = maxThinkingTokens
			}
		}
	}

	var tools []*genai.Tool
	var toolConfig *genai.ToolConfig
	if g.spec.DisableTools == nil || !*g.spec.DisableTools {
		// Collect all function declarations from state and config into a
		// single slice, then sort globally by name. Global sorting maximizes
		// prefix cache reuse: adding a function from any source inserts it
		// at its natural alphabetical position, shifting only the functions
		// that follow. See TheoryOfPrefixCaching for rationale.
		var allFuncs []FuncDecl
		for fn := range ret.Functions() {
			allFuncs = append(allFuncs, fn.Decl)
		}
		for set := range configs.All[[]FuncDecl](g.Loader(), "functions") {
			allFuncs = append(allFuncs, set...)
		}
		sort.SliceStable(allFuncs, func(i, j int) bool {
			return allFuncs[i].Name < allFuncs[j].Name
		})
		var funcDecls []*genai.FunctionDeclaration
		for _, fn := range allFuncs {
			funcDecls = append(funcDecls, fn.ToGemini())
		}
		if (g.spec.DisableSearch == nil || !*g.spec.DisableSearch) && len(funcDecls) == 0 {
			tools = append(tools, &genai.Tool{
				GoogleSearch: &genai.GoogleSearch{},
			})
		}
		if len(funcDecls) > 0 {
			tools = append(tools, &genai.Tool{
				FunctionDeclarations: funcDecls,
			})
			toolConfig = &genai.ToolConfig{
				FunctionCallingConfig: &genai.FunctionCallingConfig{
					Mode: genai.FunctionCallingConfigModeAuto,
				},
			}
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
	for content := range ret.Contents() {
		if content.Role == RoleLog || content.Role == RoleSystem {
			continue
		}
		role := string(content.Role)
		if role == string(RoleAssistant) {
			role = string(RoleModel)
		} else if role == string(RoleTool) {
			role = "function"
		}
		pbContent := &genai.Content{
			Role: role,
		}
		for _, part := range content.Parts {
			// Thoughts are only sent to the server when PreservedThinking is
			// enabled. By default, reasoning content is stripped from outgoing
			// requests to avoid sending it back to the model.
			if thought, isThought := part.(Thought); isThought {
				if g.spec.PreservedThinking != nil && *g.spec.PreservedThinking && len(thought) > 0 {
					pbContent.Parts = append(pbContent.Parts, &genai.Part{
						Text:    string(thought),
						Thought: true,
					})
				}
				continue
			}
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
	if g.spec.Temperature != nil {
		temperature = float32(*g.spec.Temperature)
	}
	if *temperatureFlag != 0 {
		temperature = float32(*temperatureFlag)
	}

	serviceTier := genai.ServiceTier(g.spec.ServiceTier)
	if serviceTier == "" {
		serviceTier = genai.ServiceTierStandard
	}

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: maxOutputTokens,
		Temperature:     &temperature,
		ThinkingConfig:  thinkingConfig,
		SafetySettings:  safetySettings,
		Tools:           tools,
		ToolConfig:      toolConfig,
		ServiceTier:     serviceTier,
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
			"model", g.spec.Model,
			"non_streaming", nonStreaming,
		)

		newState := ret
		hasContent := false
		var terminalReason string

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

			if isTerminalFinishReason(candidate.FinishReason) {
				terminalReason = string(candidate.FinishReason)
			}

			if candidate.Content != nil {
				newContent := &Content{
					Role: Role(candidate.Content.Role),
				}
				for _, part := range candidate.Content.Parts {
					if p, err := PartFromGemini(part); err != nil {
						return err
					} else if p != nil {
						hasContent = true
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
			resp, err := client.Models.GenerateContent(ctx, g.spec.Model, contents, config)
			if err != nil {
				return ret, wrap(err)
			}
			if err := handleResponse(resp); err != nil {
				return ret, err
			}

		} else {
			for msg, err := range client.Models.GenerateContentStream(ctx, g.spec.Model, contents, config) {
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
			if terminalReason != "" {
				return ret, fmt.Errorf("terminal finish reason: %s", terminalReason)
			}
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

const TheoryOfRetry = `
Retry with exponential backoff handles transient API failures (rate limits,
server errors, and empty outputs) by waiting progressively longer between
attempts. After exhausting all retries, ErrRetryable is stripped from the
returned error to break outer retry loops that would otherwise re-trigger
indefinitely. The initial backoff duration is parameterized so tests can run
without real-time delays while production callers use a meaningful delay.
`

func doWithRetry[T any](
	ctx context.Context,
	logger logs.Logger,
	fn func() (T, error),
	backoff ...time.Duration,
) (ret T, err error) {
	const maxRetries = 10
	initialBackoff := time.Second
	if len(backoff) > 0 {
		initialBackoff = backoff[0]
	}

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
			case <-time.After(initialBackoff * time.Duration(1<<i)):
			}
			continue
		}
		return ret, err
	}

	// All retries exhausted. Strip ErrRetryable from the returned error to
	// prevent outer retry loops (e.g., BuildGenerate's for-loop in
	// phases/generate.go) from re-triggering indefinitely. Without this,
	// a persistent "no output" condition would cause the model to appear
	// stuck in an infinite output loop, as BuildGenerate would keep calling
	// Generate, which would keep calling doWithRetry, which would keep
	// exhausting and returning ErrRetryable.
	// See TheoryOfGenerateRetry in phases/generate.go.
	if errors.Is(err, ErrRetryable) {
		err = fmt.Errorf("retry exhausted after %d attempts: %v", maxRetries, err)
	}
	return
}

func isRetryable(err error) bool {
	if errors.Is(err, ErrRetryable) {
		return true
	}
	var apiErr *genai.APIError
	if errors.As(err, &apiErr) {
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
			HTTPClient: httpClient.Client,
		})
		if err != nil {
			return nil, err
		}

		v, _ := clients.LoadOrStore(key, client)
		return v.(*genai.Client), nil
	}
}

type NewGemini func(spec Spec) Gemini

func (Module) NewGemini(
	inject dscope.InjectStruct,
) NewGemini {
	return func(spec Spec) Gemini {
		ret := Gemini{
			spec: spec,
		}
		inject(&ret)
		return ret
	}
}
