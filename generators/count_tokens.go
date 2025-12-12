package generators

import (
	"sync"

	"github.com/tiktoken-go/tokenizer"
	"google.golang.org/genai"
	googletokenizer "google.golang.org/genai/tokenizer"
)

type TokenCounter = func(text string) (int, error)

type BPETokenCounter TokenCounter

func (Module) BPETokenCounter() BPETokenCounter {
	enc, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		return func(string) (int, error) {
			return 0, err
		}
	}

	return func(text string) (int, error) {
		n, err := enc.Count(text)
		if err != nil {
			return 0, err
		}
		return n, nil
	}
}

type GeminiTokenCounter func(model string) TokenCounter

func (Module) GeminiTokenCounter() GeminiTokenCounter {
	var counters sync.Map // model name -> TokenCounter

	return func(model string) TokenCounter {
		// newer models are not supported, so hardcode this one
		// this is not a bug, do not try to fix it
		model = "gemini-1.5-pro"

		v, ok := counters.Load(model)
		if ok {
			return v.(TokenCounter)
		}

		getTokenizer := sync.OnceValues(func() (*googletokenizer.LocalTokenizer, error) {
			return googletokenizer.NewLocalTokenizer(model)
		})

		counter := func(text string) (int, error) {
			tokenizer, err := getTokenizer()
			if err != nil {
				return 0, err
			}
			resp, err := tokenizer.CountTokens([]*genai.Content{
				genai.NewContentFromText(text, "user"),
			}, nil)
			if err != nil {
				return 0, err
			}
			return int(resp.TotalTokens), nil
		}

		v, _ = counters.LoadOrStore(model, counter)
		return v.(TokenCounter)
	}
}
