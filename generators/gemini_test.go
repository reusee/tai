package generators

import (
	"context"
	"net"
	"os"
	"testing"

	generativelanguage "cloud.google.com/go/ai/generativelanguage/apiv1beta"
	"cloud.google.com/go/ai/generativelanguage/apiv1beta/generativelanguagepb"
	"github.com/reusee/dscope"
	"github.com/reusee/tai/configs"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/nets"
	"github.com/reusee/tai/vars"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
)

func TestGemini(t *testing.T) {
	testGenerator(t, func(
		newGemini NewGemini,
	) Generator {
		generator := newGemini(GeneratorArgs{
			Model:             "models/gemini-flash-latest",
			ContextTokens:     1 * M,
			MaxGenerateTokens: vars.PtrTo(64 * K),
			Temperature:       vars.PtrTo[float32](0.1),
			DisableSearch:     true,
		})
		return generator
	})
}

func TestGeminiListModels(t *testing.T) {
	loader := configs.NewLoader([]string{}, "")
	dscope.New(
		modes.ForTest(t),
		&loader,
		new(Module),
	).Fork(
		func() nets.ProxyAddr {
			return nets.ProxyAddr(os.Getenv("TAI_TEST_PROXY"))
		},
	).Call(func(
		dialer nets.Dialer,
		apiKey GoogleAPIKey,
	) {
		ctx := t.Context()

		clientOptions := []option.ClientOption{
			option.WithAPIKey(string(apiKey)),
			option.WithGRPCDialOption(
				grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
					return dialer.DialContext(ctx, "tcp", addr)
				}),
			),
		}
		client, err := generativelanguage.NewModelClient(ctx, clientOptions...)
		if err != nil {
			t.Fatal(err)
		}

		iter := client.ListModels(ctx, &generativelanguagepb.ListModelsRequest{
			PageSize: 1000,
		})
		for model, err := range iter.All() {
			if err != nil {
				t.Fatal(err)
			}
			_ = model
		}

	})
}
