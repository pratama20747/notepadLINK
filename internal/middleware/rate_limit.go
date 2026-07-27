// Package middleware berisi middleware Gin, termasuk validasi JWT access token
// dan rate limiting untuk endpoint auth.
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ulule/limiter/v3"
	ginlimiter "github.com/ulule/limiter/v3/drivers/middleware/gin"
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

// NewRateLimiter membuat rate limiter dengan format period yang didukung:
//   - "5-S"  = 5 request per detik
//   - "10-M" = 10 request per menit
//   - "100-H" = 100 request per jam
//   - "1000-D" = 1000 request per hari
//
// Key default adalah IP address dari request.
func NewRateLimiter(rateStr string) (gin.HandlerFunc, error) {
	rate, err := limiter.NewRateFromFormatted(rateStr)
	if err != nil {
		return nil, err
	}

	store := memory.NewStore()
	instance := limiter.New(store, rate)

	middleware := ginlimiter.NewMiddleware(instance,
		ginlimiter.WithLimitReachedHandler(func(c *gin.Context) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "terlalu banyak percobaan, coba lagi beberapa saat",
			})
			c.Abort()
		}),
	)

	return middleware, nil
}
