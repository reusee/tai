package logs

import (
	"context"
	"log/slog"
)

type Handler struct {
	slog.Handler
}

func (h *Handler) Handle(ctx context.Context, record slog.Record) error {
	if v := ctx.Value(SpanKey); v != nil {
		record.Add("logs.span", v.(Span))
	}
	return h.Handler.Handle(ctx, record)
}
