// Package middleware berisi middleware Gin untuk rate limiting dan
// structured logging untuk audit & monitoring.
package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger mengembalikan middleware Gin yang mencatat setiap request dalam format
// structured log menggunakan slog (stdlib Go 1.21+).
//
// Field yang dicatat:
//   - method, path, status, latency: info request dasar
//   - ip: IP client (untuk audit & rate limit debugging)
//   - user_agent: device/browser (untuk tracking lintas device)
//   - error: pesan error Gin jika ada
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		if query != "" {
			path = path + "?" + query
		}

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("latency", latency),
			slog.String("ip", c.ClientIP()),
			slog.String("user_agent", c.Request.UserAgent()),
		}

		if len(c.Errors) > 0 {
			attrs = append(attrs, slog.String("error", c.Errors.String()))
		}

		// Pilih log level berdasarkan status code
		switch {
		case status >= 500:
			slog.LogAttrs(c.Request.Context(), slog.LevelError, "request", attrs...)
		case status >= 400:
			slog.LogAttrs(c.Request.Context(), slog.LevelWarn, "request", attrs...)
		default:
			slog.LogAttrs(c.Request.Context(), slog.LevelInfo, "request", attrs...)
		}
	}
}
