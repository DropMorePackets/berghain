package main

import (
	"context"
	"log/slog"
	"os"
)

type logHandler struct {
	*slog.TextHandler
}

func (lh *logHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, k := range []string{
		"handler",
		"host",
		"src",
	} {
		if handler, ok := ctx.Value(k).(string); ok {
			r.AddAttrs(slog.String(k, handler))
		}
	}
	if level, ok := ctx.Value("level").(int); ok {
		r.AddAttrs(slog.Int("level", level))
	}

	return lh.TextHandler.Handle(ctx, r)
}

func Fatal(message string, args ...any) {
	slog.Error(message, args...)
	os.Exit(1)
}
