package logs

import (
	"context"
	"errors"
	"fmt"
)

func WrapSpan(ctx context.Context, err error) error {
	v := ctx.Value(SpanKey)
	if v == nil {
		return err
	}
	err = errors.Join(err, fmt.Errorf("span: %s", v.(Span)))
	return err
}
