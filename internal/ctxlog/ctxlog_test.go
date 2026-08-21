package ctxlog

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestFromOrFallback(t *testing.T) {
	fb := slog.Default()
	if got := FromOr(context.Background(), fb); got != fb {
		t.Fatalf("FromOr without ctx logger = %v, want fallback", got)
	}
}

func TestFromOrNilCtxLoggerFallsBack(t *testing.T) {
	fb := slog.Default()
	ctx := With(context.Background(), nil)
	if got := FromOr(ctx, fb); got != fb {
		t.Fatalf("FromOr with nil ctx logger = %v, want fallback", got)
	}
}

func TestFromOrCtxWins(t *testing.T) {
	lg := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := With(context.Background(), lg)
	if got := FromOr(ctx, slog.Default()); got != lg {
		t.Fatalf("FromOr = %v, want ctx logger", got)
	}
}
