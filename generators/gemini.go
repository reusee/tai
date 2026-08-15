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
	spec            Spec
	GetClient       dscope.Inject[GetGeminiClient]
	APIKey          dscope.Inject[GoogleAPIKey]
	Counter         dscope.Inject[GeminiTokenCounter]
	Logger          dscope.Inject[logs.Logger]
	Effort          dscope.Inject[EffortFlag]
	TemperatureFlag dscope.Inject[TemperatureFlag]
	Debug           dscope.Inject[DebugGemini]
	FuncDecls       dscope.Inject[FuncDecls]
	EventRecorder   dscope.Inject[EventRecorder]
	Retrier         dscope.Inject[Retrier]
}

var _ Generator = Gemini{}

func (g Gemini) Spec() Spec {
	return g.spec
}

// recordEvent records an API-level event in the interaction transcript
// when interaction recording is active. The recorder is injected as a
// dscope dependency when the generator is constructed; the tai command
// forks the EventRecorder provider with the records.Recorder value. See
// generators.TheoryOfEventRecorder.
func (g Gemini) recordEvent(typ string, detail string) {
	if rec := g.EventRecorder(); rec != nil && rec.Enabled() {
		rec.Event(typ, detail)
	}
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
		allFuncs = append(allFuncs, g.FuncDecls()...)
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
			if pbPart := partToGemini(part); pbPart != nil {
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
	if flag := g.TemperatureFlag(); flag.Value != nil {
		temperature = *flag.Value
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

	ret, err = g.Retrier().Do(ctx, func() (State, error) {

		g.Logger().InfoContext(ctx, "generating",
			"name", g.spec.Name,
			"model", g.spec.Model,
			"effort", g.spec.ReasoningEffort,
			"non_streaming", nonStreaming,
		)
		g.recordEvent("api_call", fmt.Sprintf("gemini generate content: model=%s effort=%s non_streaming=%v", g.spec.Model, g.spec.ReasoningEffort, nonStreaming))

		newState := ret
		hasContent := false
		var terminalReason string

		handleResponse := func(resp *genai.GenerateContentResponse) error {
			if g.Debug() {
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
					if p := PartFromGemini(part); p != nil {
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
				g.recordEvent("api_error", fmt.Sprintf("gemini non-streaming API call failed: %v", err))
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
					g.recordEvent("api_error", fmt.Sprintf("gemini streaming API call failed: %v", err))
					return ret, wrap(err)
				}
				if err := handleResponse(msg); err != nil {
					return ret, err
				}
			}
		}

		if !hasContent {
			if terminalReason != "" {
				g.recordEvent("api_error", fmt.Sprintf("gemini terminal finish reason: %s", terminalReason))
				return ret, fmt.Errorf("terminal finish reason: %s", terminalReason)
			}
			g.recordEvent("api_error", "gemini returned no output")
			// no output
			return ret, fmt.Errorf("no output")
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
Retry with exponential backoff handles transient API failures (rate limits
and server errors) by waiting progressively longer between attempts. After
exhausting all retries, ErrRetryable is stripped from the returned error to
break outer retry loops that would otherwise re-trigger indefinitely. The
initial backoff duration is parameterized so tests can run without real-time
delays while production callers use a meaningful delay.

Retrier carries this logic as a Go 1.27 generic method: Do is generic over
the result type, so one dscope-provided value serves every caller. Before
generic methods, the same shape required a package-level generic function
plus a non-generic function type that fixed the type parameter to State;
the method subsumes both, and callers pass only the runtime values
(context, the function, and the optional backoff).
`

// Retrier carries the retry dependencies — the logger and the interaction
// event recorder — bound from the dscope scope at provider resolution time.
// Its Do method is generic over the result type, so the same retry logic
// serves every caller; Gemini.Generate uses it with State. Callers pass
// only the runtime values. See TheoryOfRetry.
type Retrier struct {
	logger        logs.Logger
	eventRecorder EventRecorder
}

// Retrier provider: binds the logger and the interaction event recorder
// from the dscope scope. See TheoryOfRetry and TheoryOfEventRecorder.
func (Module) Retrier(
	logger logs.Logger,
	eventRecorder EventRecorder,
) Retrier {
	return Retrier{
		logger:        logger,
		eventRecorder: eventRecorder,
	}
}

// Do runs fn with retry and exponential backoff. The result type is
// inferred from fn, so the method is called without explicit
// instantiation. After exhausting all retries, ErrRetryable is stripped
// from the returned error to break outer retry loops.
// See TheoryOfRetry.
func (r Retrier) Do[T any](
	ctx context.Context,
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
			r.logger.WarnContext(ctx, "retry",
				"attempt", i+1, "error", err,
			)
			// Each retryable API error is recorded so the interaction
			// transcript shows the raw API errors even when a later
			// attempt succeeds. The recorder is bound at provider
			// resolution. See generators.TheoryOfEventRecorder.
			if r.eventRecorder != nil && r.eventRecorder.Enabled() {
				r.eventRecorder.Event("api_error", fmt.Sprintf("retryable API error (attempt %d/%d): %v", i+1, maxRetries, err))
			}
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
	// phases/generate.go) from re-triggering indefinitely.
	// See TheoryOfGenerateRetry in phases/generate.go.
	if errors.Is(err, ErrRetryable) {
		if r.eventRecorder != nil && r.eventRecorder.Enabled() {
			r.eventRecorder.Event("api_error", fmt.Sprintf("API retry exhausted after %d attempts: %v", maxRetries, err))
		}
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
