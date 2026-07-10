package generators

import (
	"sync"
	"unicode"

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
		model = "gemini-3-pro-preview"

		if v, ok := counters.Load(model); ok {
			return v.(TokenCounter)
		}

		// Lazy initialization structure per model
		type tokenizerCache struct {
			once sync.Once
			tok  *googletokenizer.LocalTokenizer
			err  error
		}
		cache := &tokenizerCache{}

		counter := func(text string) (int, error) {
			cache.once.Do(func() {
				cache.tok, cache.err = googletokenizer.NewLocalTokenizer(model)
			})
			if cache.err != nil {
				return 0, cache.err
			}
			resp, err := cache.tok.CountTokens([]*genai.Content{
				genai.NewContentFromText(text, "user"),
			}, nil)
			if err != nil {
				return 0, err
			}
			return int(resp.TotalTokens), nil
		}

		actual, loaded := counters.LoadOrStore(model, counter)
		if loaded {
			return actual.(TokenCounter)
		}
		return counter
	}
}

var DeepseekTokenCounterFn TokenCounter = func(text string) (int, error) {
	var total float64
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			total += 0.6
		} else {
			total += 0.3
		}
	}
	return int(total), nil
}
