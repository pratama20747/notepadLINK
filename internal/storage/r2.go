// Package storage membungkus Cloudflare R2 (S3-compatible) lewat AWS SDK v2.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"notepad-sharelink/internal/config"
)

// Client membungkus S3 client yang sudah dikonfigurasi ke endpoint R2.
type Client struct {
	s3         *s3.Client
	presign    *s3.PresignClient
	bucket     string
	publicBase string
	presignTTL time.Duration
}

// NewR2Client membuat Client baru dari config aplikasi.
func NewR2Client(cfg *config.Config) (*Client, error) {
	if !cfg.R2Enabled() {
		return nil, fmt.Errorf("R2 belum dikonfigurasi (cek R2_ACCOUNT_ID dkk di .env)")
	}

	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID)

	s3Client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.R2AccessKeyID, cfg.R2SecretAccessKey, "",
		),
	})

	return &Client{
		s3:         s3Client,
		presign:    s3.NewPresignClient(s3Client),
		bucket:     cfg.R2BucketName,
		publicBase: strings.TrimRight(cfg.R2PublicBaseURL, "/"),
		presignTTL: cfg.PresignTTL,
	}, nil
}

// PublicURL membangun URL publik dari key R2.
func (c *Client) PublicURL(key string) string {
	return c.publicBase + "/" + strings.TrimLeft(key, "/")
}

// GeneratePresignedPutURL membuat presigned URL untuk upload langsung dari
// frontend (browser PUT file ke URL ini, tanpa lewat backend Go).
func (c *Client) GeneratePresignedPutURL(ctx context.Context, key, contentType string) (string, error) {
	req, err := c.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(c.presignTTL))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PutObject upload langsung dari backend (dipakai untuk attachment note
// private, karena backend perlu enkripsi dulu sebelum file sampai ke R2).
func (c *Client) PutObject(ctx context.Context, key, contentType string, body []byte) (string, error) {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", err
	}
	return c.PublicURL(key), nil
}

// HeadObject mengembalikan ukuran & content-type AKTUAL yang tersimpan di R2.
func (c *Client) HeadObject(ctx context.Context, key string) (size int64, contentType string, err error) {
	out, err := c.s3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, "", err
	}
	if out.ContentLength != nil {
		size = *out.ContentLength
	}
	if out.ContentType != nil {
		contentType = *out.ContentType
	}
	return size, contentType, nil
}

// GetObjectRange mengambil n byte pertama dari object — dipakai untuk deteksi
// magic-bytes tanpa perlu download seluruh file (penting untuk video besar).
func (c *Client) GetObjectRange(ctx context.Context, key string, n int64) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=0-%d", n-1)),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// GetObject mendownload seluruh object — dipakai untuk attachment private
// (perlu byte lengkap sebelum bisa didekripsi, AES-GCM tidak streaming-friendly
// untuk MVP ini).
func (c *Client) GetObject(ctx context.Context, key string) ([]byte, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// DeleteObject menghapus object dari R2.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	return err
}

// MirrorFromURL mendownload file dari URL eksternal (mis. foto profil Google)
// lalu upload ke R2.
func (c *Client) MirrorFromURL(ctx context.Context, sourceURL, destKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("gagal download sumber avatar, status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 10*1024*1024)
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	return c.PutObject(ctx, destKey, contentType, data)
}

// RandomHex menghasilkan string hex acak pendek untuk dipakai di nama key
// (menghindari collision tanpa perlu dependency uuid tambahan).
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
