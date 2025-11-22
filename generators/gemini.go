package generators

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	generativelanguage "cloud.google.com/go/ai/generativelanguage/apiv1beta"
	"cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/vars"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func (g Gemini) Generate(ctx context.Context, state State) (ret State, err error) {
	client, err := g.GetClient()(ctx, g.args.APIKey)
	if err != nil {
		return ret, err
	}

	ret = state

	var maxOutputTokens *int32
	var maxThinkingTokens *int32
	if g.args.MaxGenerateTokens != nil {
		max := int32(*g.args.MaxGenerateTokens)
		maxOutputTokens = &max
		maxThinking := int32(*g.args.MaxGenerateTokens) / 4
		maxThinkingTokens = &maxThinking
	}

	var tools []*generativelanguagepb.Tool
	if !g.args.DisableSearch && len(ret.FuncMap()) == 0 {
		tools = append(tools, &generativelanguagepb.Tool{
			GoogleSearch: &generativelanguagepb.Tool_GoogleSearch{},
		})
	}

	var funcDecls []*generativelanguagepb.FunctionDeclaration
	for _, fn := range ret.FuncMap() {
		funcDecls = append(funcDecls, fn.Decl.ToGemini())
	}
	for set := range configs.All[[]FuncDecl](g.Loader(), "functions") {
		for _, fn := range set {
			funcDecls = append(funcDecls, fn.ToGemini())
		}
	}
	var functionCallingConfig *generativelanguagepb.FunctionCallingConfig
	if len(funcDecls) > 0 {
		tools = append(tools, &generativelanguagepb.Tool{
			FunctionDeclarations: funcDecls,
		})
		functionCallingConfig = &generativelanguagepb.FunctionCallingConfig{
			Mode: generativelanguagepb.FunctionCallingConfig_VALIDATED,
		}
	}

	safetySettings := []*generativelanguagepb.SafetySetting{
		{
			Category:  generativelanguagepb.HarmCategory_HARM_CATEGORY_HATE_SPEECH,
			Threshold: generativelanguagepb.SafetySetting_OFF,
		},
		{
			Category:  generativelanguagepb.HarmCategory_HARM_CATEGORY_SEXUALLY_EXPLICIT,
			Threshold: generativelanguagepb.SafetySetting_OFF,
		},
		{
			Category:  generativelanguagepb.HarmCategory_HARM_CATEGORY_DANGEROUS_CONTENT,
			Threshold: generativelanguagepb.SafetySetting_OFF,
		},
		{
			Category:  generativelanguagepb.HarmCategory_HARM_CATEGORY_HARASSMENT,
			Threshold: generativelanguagepb.SafetySetting_OFF,
		},
		{
			Category:  generativelanguagepb.HarmCategory_HARM_CATEGORY_CIVIC_INTEGRITY,
			Threshold: generativelanguagepb.SafetySetting_OFF,
		},
	}

	var contents []*generativelanguagepb.Content
	for _, content := range ret.Contents() {
		role := content.Role
		if role == RoleAssistant {
			// convert to gemini role
			role = RoleModel
		}
		pbContent := generativelanguagepb.Content{
			Role: string(role),
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
			contents = append(contents, &pbContent)
		}
	}

	temperature := g.args.Temperature
	if *temperatureFlag != 0 {
		temperature = temperatureFlag
	}

	req := &generativelanguagepb.GenerateContentRequest{
		Model: g.args.Model,
		Tools: tools,
		ToolConfig: &generativelanguagepb.ToolConfig{
			FunctionCallingConfig: functionCallingConfig,
		},
		SafetySettings: safetySettings,
		GenerationConfig: &generativelanguagepb.GenerationConfig{
			MaxOutputTokens: maxOutputTokens,
			Temperature:     temperature,
			ThinkingConfig: &generativelanguagepb.ThinkingConfig{
				IncludeThoughts: vars.PtrTo(true),
				ThinkingBudget:  maxThinkingTokens,
			},
		},
		Contents: contents,
		SystemInstruction: &generativelanguagepb.Content{
			Role: string(RoleSystem),
			Parts: []*generativelanguagepb.Part{
				{
					Data: &generativelanguagepb.Part_Text{
						Text: ret.SystemPrompt(),
					},
				},
			},
		},
	}

	ret, err = doWithRetry(ctx, g.Logger(), func() (State, error) {

		g.Logger().InfoContext(ctx, "generating",
			"model", g.args.Model,
		)

		streamClient, err := client.StreamGenerateContent(ctx, req)
		if err != nil {
			return ret, err
		}
		defer streamClient.CloseSend()

		newState := ret
		hasContent := false

		for {
			select {
			case <-ctx.Done():
				return ret, ctx.Err()
			default:
			}

			resp, err := streamClient.Recv()
			if err == io.EOF {
				if !hasContent {
					// no output
					return ret, errors.Join(fmt.Errorf("no output"), ErrRetryable)
				}
				break
			}
			if err != nil {
				return ret, wrap(err)
			}

			if *debugGemini {
				g.Logger().InfoContext(ctx, "gemini response",
					"details", resp,
				)
			}

			if metadata := resp.GetUsageMetadata(); metadata != nil {
				var usage Usage
				usage.Prompt.TokenCount = int(metadata.PromptTokenCount)
				usage.Prompt.TokenCountCached = int(metadata.CachedContentTokenCount)
				usage.Candidates.TokenCount = int(metadata.CandidatesTokenCount)
				usage.Thoughts.TokenCount = int(metadata.ThoughtsTokenCount)
				newState, err = newState.AppendContent(&Content{
					Role:  RoleLog,
					Parts: []Part{usage},
				})
				if err != nil {
					return ret, err
				}
			}

			if len(resp.Candidates) == 0 {
				continue
			}
			candidate := resp.Candidates[0]

			if candidate.Content != nil {
				newContent := &Content{
					Role: Role(candidate.Content.Role),
				}
				for _, part := range candidate.Content.Parts {
					if p, err := PartFromGemini(part); err != nil {
						return ret, err
					} else if p != nil {
						if _, isThought := p.(Thought); !isThought {
							hasContent = true
						}
						newContent.Parts = append(newContent.Parts, p)
					}
				}
				if newState, err = newState.AppendContent(newContent); err != nil {
					return ret, err
				}
			}

			if reason := candidate.GetFinishReason(); reason > 0 {
				if newState, err = newState.AppendContent(&Content{
					Role: RoleLog,
					Parts: []Part{
						FinishReason(reason.String()),
					},
				}); err != nil {
					return ret, err
				}
			}

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
	s, ok := status.FromError(err)
	if ok && (s.Code() == codes.ResourceExhausted || s.Code() == codes.Unavailable) {
		return true
	}
	if errors.Is(err, ErrRetryable) {
		return true
	}
	return false
}

type GetGeminiClient = func(ctx context.Context, key string) (*generativelanguage.GenerativeClient, error)

func (Module) GetGeminiClient(
	dialer nets.Dialer,
	apiKey GoogleAPIKey,
) GetGeminiClient {
	var clients sync.Map // key -> *generativelanguage.GenerativeClient
	return func(ctx context.Context, key string) (*generativelanguage.GenerativeClient, error) {
		key = vars.FirstNonZero(
			key,
			string(apiKey),
		)

		if v, ok := clients.Load(key); ok {
			return v.(*generativelanguage.GenerativeClient), nil
		}

		clientOptions := []option.ClientOption{
			option.WithAPIKey(key),
			option.WithGRPCDialOption(
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					return dialer.DialContext(ctx, "tcp", addr)
				}),
			),
		}
		client, err := generativelanguage.NewGenerativeClient(ctx, clientOptions...)
		if err != nil {
			return nil, err
		}

		v, loaded := clients.LoadOrStore(key, client)
		if loaded {
			// not store
			client.Close()
		}

		return v.(*generativelanguage.GenerativeClient), nil
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
