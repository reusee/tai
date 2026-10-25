package generators

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/reusee/tai/logs"
)

// transportFailGenerator fails its first request with a closed connection and
// succeeds afterwards, so the phase's retry decision is observable.
type transportFailGenerator struct {
	calls int
}

func (g *transportFailGenerator) Spec() Spec {
	return Spec{Model: "fake"}
}

func (g *transportFailGenerator) CountTokens(string) (int, error) {
	return 0, nil
}

func (g *transportFailGenerator) Generate(
	ctx context.Context,
	state State,
	options *GenerateOptions,
) (State, error) {
	g.calls++
	if g.calls == 1 {
		return state, OpenAIError{Err: fmt.Errorf("post: %w", net.ErrClosed)}
	}
	return state, nil
}

// TestBuildGenerateRetriesTransientTransportError is the reproduction of the
// dropped-connection failure: an OpenAI-protocol request has no internal
// retry, so the phase is its only retry, and a marker-only test used to end
// the run on the first closed connection. See TheoryOfGenerateRetry.
func TestBuildGenerateRetriesTransientTransportError(t *testing.T) {
	logger := logs.Logger{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	generator := new(transportFailGenerator)
	build := Module{}.BuildGenerate(logger)

	_, _, err := build(generator, nil)(nil)(context.Background(), NewPrompts("", nil))
	if err != nil {
		t.Fatalf("a transient transport fault ended the phase: %v", err)
	}
	if generator.calls != 2 {
		t.Fatalf("generator ran %d time(s), want 2", generator.calls)
	}
}
