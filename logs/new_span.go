package logs

import (
	"context"
	"crypto/rand"
)

type NewSpan func(ctx context.Context, parent Span) (context.Context, Span)

func (Module) NewSpan(
	logger Logger,
) NewSpan {
	return func(ctx context.Context, parent Span) (context.Context, Span) {

		// creator
		var creatorSpan Span
		if v := ctx.Value(SpanKey); v != nil {
			creatorSpan = v.(Span)
		}
		if parent == "" {
			parent = creatorSpan
		}

		// span
		span := Span(rand.Text())
		ctx = context.WithValue(ctx, SpanKey, span)

		// logs
		var args []any
		if creatorSpan != "" && creatorSpan != parent {
			args = append(args, "creator", creatorSpan)
		}
		if parent != "" {
			args = append(args, "parent", parent)
		}
		logger.InfoContext(ctx, "new span", args...)

		return ctx, span
	}
}
