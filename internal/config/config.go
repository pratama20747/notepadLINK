// Package config memuat konfigurasi aplikasi dari environment variable / file .env.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config menyimpan seluruh konfigurasi yang dibutuhkan aplikasi.
type Config struct {
	DatabaseURL    string
	Port           string
	IsProd         bool
	AllowedOrigins []string
	RequestTimeout time.Duration
	// Cloudflare R2
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2BucketName      string
	R2PublicBaseURL   string
	PresignTTL        time.Duration

	// Limit ukuran file, dalam byte (diturunkan dari env dalam MB)
	MaxImageAttachSize    int64
	MaxVideoAttachSize    int64
	MaxAttachmentsPerNote int
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("environment variable DATABASE_URL wajib di-set")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	isProd := os.Getenv("APP_ENV") == "production"

	allowedOrigins := []string{}
	if origins := os.Getenv("ALLOWED_ORIGINS"); origins != "" {
		allowedOrigins = strings.Split(origins, ",")
	}

	// --- R2 ---
	r2AccountID := os.Getenv("R2_ACCOUNT_ID")
	r2AccessKeyID := os.Getenv("R2_ACCESS_KEY_ID")
	r2SecretAccessKey := os.Getenv("R2_SECRET_ACCESS_KEY")
	r2BucketName := os.Getenv("R2_BUCKET_NAME")
	r2PublicBaseURL := os.Getenv("R2_PUBLIC_BASE_URL")

	presignMinutes := envInt("PRESIGN_TTL_MINUTES", 10)

	maxImageMB := envInt("MAX_IMAGE_ATTACHMENT_SIZE_MB", 10)
	maxVideoMB := envInt("MAX_VIDEO_ATTACHMENT_SIZE_MB", 50)
	maxAttachments := envInt("MAX_ATTACHMENTS_PER_NOTE", 10)

	return &Config{
		DatabaseURL:       dbURL,
		Port:              port,
		IsProd:            isProd,
		AllowedOrigins:    allowedOrigins,
		RequestTimeout:    time.Second * 30,
		R2AccountID:       r2AccountID,
		R2AccessKeyID:     r2AccessKeyID,
		R2SecretAccessKey: r2SecretAccessKey,
		R2BucketName:      r2BucketName,
		R2PublicBaseURL:   r2PublicBaseURL,
		PresignTTL:        time.Duration(presignMinutes) * time.Minute,

		MaxImageAttachSize:    int64(maxImageMB) * 1024 * 1024,
		MaxVideoAttachSize:    int64(maxVideoMB) * 1024 * 1024,
		MaxAttachmentsPerNote: maxAttachments,
	}, nil
}

// R2Enabled mengecek apakah kredensial R2 sudah dikonfigurasi.
func (c *Config) R2Enabled() bool {
	return c.R2AccountID != "" && c.R2AccessKeyID != "" && c.R2SecretAccessKey != "" && c.R2BucketName != ""
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
