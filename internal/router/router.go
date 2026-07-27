package router

import (
	"os"

	"notepad-sharelink/internal/handler"
	"notepad-sharelink/internal/middleware"

	"github.com/gin-gonic/gin"
)

// New membangun router aplikasi. Tidak ada lagi konsep akun/login — semua
// endpoint note & attachment bersifat publik, diakses lewat ID (share link)
// dan, untuk mode private, password.
func New(
	noteHandler *handler.NoteHandler,
	attachmentHandler *handler.AttachmentHandler,
) *gin.Engine {
	r := gin.New()

	r.Use(middleware.Logger())
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())

	r.GET("/health", healthCheck)

	// Rate limiter untuk endpoint yang rawan brute-force / abuse.
	createLimiter, _ := middleware.NewRateLimiter("5-M")
	unlockLimiter, _ := middleware.NewRateLimiter("20-M")

	notes := r.Group("/api/notes")
	{
		notes.GET("/:id", noteHandler.Get)
		notes.POST("/:id/unlock", unlockLimiter, noteHandler.Unlock)
		notes.POST("", createLimiter, noteHandler.Create)
		notes.PUT("/:id", noteHandler.Update)
		notes.DELETE("/:id", noteHandler.Delete)

		// Attachments
		notes.POST("/:id/attachments/presign", attachmentHandler.PresignUpload)
		notes.POST("/:id/attachments/confirm", attachmentHandler.ConfirmUpload)
		notes.POST("/:id/attachments/private", attachmentHandler.UploadPrivate)
		notes.POST("/attachments/:attachmentId/download", attachmentHandler.DownloadPrivate)
		notes.DELETE("/attachments/:attachmentId", attachmentHandler.Delete)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(404, gin.H{"error": "Not found"})
	})

	return r
}

// CORS middleware
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if os.Getenv("APP_ENV") == "development" {
			if origin != "" {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if c.Request.Method == "OPTIONS" {
				c.AbortWithStatus(204)
				return
			}
			c.Next()
			return
		}

		// PRODUCTION: Allowlist
		allowed := map[string]bool{
			"https://pratama20747.github.io": true,
			"https://binery.my.id":           true,
		}
		if allowed[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{"status": "ok"})
}
