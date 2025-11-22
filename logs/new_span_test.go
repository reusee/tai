package logs

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/reusee/dscope"
)

func TestNewSpan(t *testing.T) {
	buf := new(bytes.Buffer)
	dscope.New(new(Module)).Fork(
		func() Writer {
			return buf
		},
	).Call(func(
		newSpan NewSpan,
	) {
		ctx := context.Background()

		ctx1, span1 := newSpan(ctx, "")

		ctx11, span11 := newSpan(ctx1, "")

		ctx12, span12 := newSpan(ctx11, span1)
		_ = ctx12

		lines := strings.Split(string(buf.Bytes()), "\n")
		if !strings.Contains(lines[0], "logs.span="+string(span1)) {
			t.Fatalf("got %v", lines[0])
		}
		if !strings.Contains(lines[1], "logs.span="+string(span11)) {
			t.Fatalf("got %v", lines[1])
		}
		if !strings.Contains(lines[2], "logs.span="+string(span12)) {
			t.Fatalf("got %v", lines[2])
		}
		if !strings.Contains(lines[1], "parent="+string(span1)) {
			t.Fatalf("got %v", lines[1])
		}
		if !strings.Contains(lines[2], "parent="+string(span1)) {
			t.Fatalf("got %v", lines[2])
		}
		if !strings.Contains(lines[2], "creator="+string(span11)) {
			t.Fatalf("got %v", lines[2])
		}

	})
}
