package router

import (
	"context"
	"net/http"
	"time"

	"notepad-sharelink/internal/config"
	"notepad-sharelink/internal/handler"
	"notepad-sharelink/internal/middleware"

	"github.com/gin-gonic/gin"
)

func New(
	noteHandler *handler.NoteHandler,
	attachmentHandler *handler.AttachmentHandler,
	cfg *config.Config,
) *gin.Engine {
	r := gin.New()

	r.Use(middleware.Logger())
	r.Use(gin.Recovery())
	r.Use(corsMiddleware(cfg.AllowedOrigins, cfg.IsProd))
	r.Use(securityHeaders())
	r.Use(timeoutMiddleware(cfg.RequestTimeout))

	r.GET("/health", healthCheck)

	// ============================================
	// RATE LIMITER - langsung pake NewRateLimiter
	// ============================================
	createLimiter, err := middleware.NewRateLimiter("5-M")
	if err != nil {
		panic("gagal init rate limiter create: " + err.Error())
	}

	unlockLimiter, err := middleware.NewRateLimiter("20-M")
	if err != nil {
		panic("gagal init rate limiter unlock: " + err.Error())
	}

	notes := r.Group("/api/notes")
	{
		notes.GET("/:id", noteHandler.Get)
		notes.POST("", createLimiter, noteHandler.Create) // ← pake limiter
		notes.PUT("/:id", noteHandler.Update)
		notes.DELETE("/:id", noteHandler.Delete)
		notes.POST("/:id/unlock", unlockLimiter, noteHandler.Unlock) // ← pake limiter
	}

	attachments := r.Group("/api/notes/:id/attachments")
	{
		attachments.POST("/presign", attachmentHandler.PresignUpload)
		attachments.POST("/confirm", attachmentHandler.ConfirmUpload)
		attachments.POST("/private", attachmentHandler.UploadPrivate)
	}

	downloads := r.Group("/api/attachments")
	{
		downloads.POST("/:attachmentId/download", attachmentHandler.DownloadPrivate)
		downloads.DELETE("/:attachmentId", attachmentHandler.Delete)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
	})

	return r
}

// ============================================
// CORS
// ============================================
func corsMiddleware(allowedOrigins []string, isProd bool) gin.HandlerFunc {
	allowedMap := make(map[string]bool)
	for _, origin := range allowedOrigins {
		allowedMap[origin] = true
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if isOriginAllowed(origin, allowedMap, isProd) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}

		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func isOriginAllowed(origin string, allowedMap map[string]bool, isProd bool) bool {
	if !isProd {
		return origin != ""
	}
	return allowedMap[origin]
}

// ============================================
// Security Headers
// ============================================
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// ============================================
// Timeout Middleware
// ============================================
func timeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// ============================================
// Health Check
// ============================================
func healthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
