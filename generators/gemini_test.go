package generators

import (
	"os"
	"testing"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"google.golang.org/genai"
)

func TestGemini(t *testing.T) {
	testGenerator(t, func(
		newGemini NewGemini,
	) Generator {
		generator := newGemini(Spec{
			Model:             "models/gemini-flash-latest",
			ContextTokens:     1 * M,
			MaxGenerateTokens: new(64 * K),
			Temperature:       new(float32(0.1)),
			DisableSearch:     new(true),
		})
		return generator
	})
}

func TestGeminiListModels(t *testing.T) {
	loader := configs.NewLoader([]string{}, configs.LoaderConfig{})
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.ProxyAddr {
			return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
		},
	).Call(func(
		httpClient nets.HTTPClient,
		apiKey GoogleAPIKey,
	) {
		ctx := t.Context()

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     string(apiKey),
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: httpClient,
		})
		if err != nil {
			t.Fatal(err)
		}

		resp, err := client.Models.List(ctx, &genai.ListModelsConfig{
			PageSize: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range resp.Items {
			_ = model
		}

	})
}
