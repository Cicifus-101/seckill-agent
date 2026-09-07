package logx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
)

type contextKey string

const requestIDKey contextKey = "request_id"

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		requestID = newRequestID()
	}
	return context.WithValue(ctx, requestIDKey, requestID)
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

func RequestIDFromHTTP(r *http.Request) string {
	if v := r.Header.Get("X-Request-Id"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Trace-Id"); v != "" {
		return v
	}
	return newRequestID()
}

func WithContext(logger *slog.Logger, ctx context.Context) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	if requestID := RequestID(ctx); requestID != "" {
		return logger.With("request_id", requestID)
	}
	return logger
}

func newRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(buf)
}
