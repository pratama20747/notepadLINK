// Package handler — attachment_handler.go menangani upload/download file
// attachment di notes (gambar & video), baik mode public (presigned R2)
// maupun private (server-side enkripsi).
package handler

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"notepad-sharelink/internal/service"
)

type AttachmentHandler struct {
	svc             *service.AttachmentService
	maxUploadMemory int64 // batas atas absolut untuk http.MaxBytesReader (video)
}

func NewAttachmentHandler(svc *service.AttachmentService, maxUploadMemory int64) *AttachmentHandler {
	return &AttachmentHandler{svc: svc, maxUploadMemory: maxUploadMemory}
}

type presignAttachmentRequest struct {
	ContentType string `json:"content_type" binding:"required"`
	FileSize    int64  `json:"file_size" binding:"required"`
	Kind        string `json:"kind" binding:"required,oneof=image video"`
}

func (h *AttachmentHandler) PresignUpload(c *gin.Context) {
	noteID := c.Param("id")
	var req presignAttachmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	uploadURL, key, err := h.svc.PresignUpload(c.Request.Context(), noteID, req.ContentType, req.FileSize, req.Kind)
	if err != nil {
		respondAttachmentError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"upload_url": uploadURL, "key": key})
}

type confirmAttachmentRequest struct {
	Key  string `json:"key" binding:"required"`
	Kind string `json:"kind" binding:"required,oneof=image video"`
}

func (h *AttachmentHandler) ConfirmUpload(c *gin.Context) {
	noteID := c.Param("id")
	var req confirmAttachmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	att, err := h.svc.ConfirmUpload(c.Request.Context(), noteID, req.Key, req.Kind)
	if err != nil {
		respondAttachmentError(c, err)
		return
	}

	c.JSON(http.StatusCreated, att)
}

// UploadPrivate menangani POST /api/notes/:id/attachments/private (multipart).
// Field form: "file" (file), "kind" (image|video), "password".
func (h *AttachmentHandler) UploadPrivate(c *gin.Context) {
	noteID := c.Param("id")

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadMemory)

	password := c.PostForm("password")
	kind := c.PostForm("kind")
	if password == "" || (kind != "image" && kind != "video") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password dan kind (image/video) wajib diisi"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file wajib diupload (field 'file')"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file terlalu besar atau gagal dibaca"})
		return
	}

	claimedType := header.Header.Get("Content-Type")

	att, err := h.svc.UploadPrivate(c.Request.Context(), noteID, password, kind, claimedType, data)
	if err != nil {
		respondAttachmentError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"attachment_index": att.AttachmentIndex, "kind": att.Kind, "content_type": att.ContentType,
		"file_size": att.FileSize, "created_at": att.CreatedAt,
	})
}

type downloadPrivateRequest struct {
	Password string `json:"password" binding:"required"`
}

// DownloadPrivate menangani POST /api/notes/:id/attachments/:index/download.
// PUBLIK (seperti Unlock note) — mengembalikan bytes file asli setelah didekripsi.
func (h *AttachmentHandler) DownloadPrivate(c *gin.Context) {
	noteID := c.Param("id")
	indexStr := c.Param("index")
	index, err := strconv.ParseInt(indexStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attachment index"})
		return
	}

	var req downloadPrivateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, contentType, err := h.svc.DownloadPrivate(c.Request.Context(), noteID, int32(index), req.Password)
	if err != nil {
		respondAttachmentError(c, err)
		return
	}

	c.Data(http.StatusOK, contentType, data)
}

func (h *AttachmentHandler) Delete(c *gin.Context) {
	noteID := c.Param("id")
	indexStr := c.Param("index")
	index, err := strconv.ParseInt(indexStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid attachment index"})
		return
	}

	if err := h.svc.Delete(c.Request.Context(), noteID, int32(index)); err != nil {
		respondAttachmentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "attachment berhasil dihapus"})
}

func respondAttachmentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "tidak ditemukan"})
	case errors.Is(err, service.ErrInvalidKey):
		c.JSON(http.StatusBadRequest, gin.H{"error": "key tidak valid"})
	case errors.Is(err, service.ErrWrongPassword):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "password salah"})
	case errors.Is(err, service.ErrContentTypeBad), errors.Is(err, service.ErrInvalidKind):
		c.JSON(http.StatusBadRequest, gin.H{"error": "tipe file tidak diizinkan"})
	case errors.Is(err, service.ErrFileTooLarge):
		c.JSON(http.StatusBadRequest, gin.H{"error": "ukuran file melebihi batas maksimal"})
	case errors.Is(err, service.ErrTooManyAttachments):
		c.JSON(http.StatusBadRequest, gin.H{"error": "jumlah attachment sudah mencapai batas maksimal untuk note ini"})
	case errors.Is(err, service.ErrUploadNotFound):
		c.JSON(http.StatusBadRequest, gin.H{"error": "file belum berhasil diupload ke storage"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
