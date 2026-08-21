// Package ctxlog carries a request-scoped *slog.Logger through a context so
// that service internals called while handling an HTTP request log with the
// same correlation fields (e.g. request_id) as the HTTP layer. This package
// exists because the in-tree log/slog does not ship the standard
// slog.NewContext/slog.FromContext helpers.
package ctxlog

import (
	"context"
	"log/slog"
)

type key struct{}

// With returns a copy of ctx carrying lg, retrievable with From.
func With(ctx context.Context, lg *slog.Logger) context.Context {
	return context.WithValue(ctx, key{}, lg)
}

// From returns the logger stored in ctx by With, or nil if none is present.
func From(ctx context.Context) *slog.Logger {
	lg, _ := ctx.Value(key{}).(*slog.Logger)
	return lg
}

// FromOr returns the logger stored in ctx by With, or fb if none is present.
func FromOr(ctx context.Context, fb *slog.Logger) *slog.Logger {
	if lg := From(ctx); lg != nil {
		return lg
	}
	return fb
}
