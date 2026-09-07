package http

import (
	"log/slog"
	"net/http"
	"seckill-agent/pkg/logx"
	"time"
)

// withLogging 补充request_id上下文，并在请求处理完成后统一打印访问日志（方法、路径、状态码、耗时、客户端地址）
func withLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := logx.RequestIDFromHTTP(r)
		ctx := logx.WithRequestID(r.Context(), requestID)
		r = r.WithContext(ctx)

		recorder := &statusRecorder{
			ResponseWriter: w,
			// 原始的http.ResponseWriter中方法WriteReader(statusCode int)状态码被写入，但是无法被外部读取
			status: http.StatusOK,
		}

		next.ServeHTTP(recorder, r)

		logx.WithContext(logger, r.Context()).InfoContext(r.Context(), "http request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote_addr", r.RemoteAddr,
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.status = statusCode                    // 1. 保存状态码
	r.ResponseWriter.WriteHeader(statusCode) // 2. 转发给原始 ResponseWriter，客户端收到响应
}
