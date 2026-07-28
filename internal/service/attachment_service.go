// Package service — attachment_service.go menangani attachment gambar/video
// di notes. Note PUBLIC pakai presigned URL langsung ke R2 (plaintext).
// Note PRIVATE di-enkripsi server-side (reuse cryptoutil, key sama dengan
// content note) — makanya upload/download-nya lewat backend, bukan presign.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"notepad-sharelink/internal/cryptoutil"
	"notepad-sharelink/internal/db/sqlc"
	"notepad-sharelink/internal/fileutil"
	"notepad-sharelink/internal/storage"

	"github.com/jackc/pgx/v5"
)

var (
	ErrInvalidKind        = errors.New("kind harus 'image' atau 'video'")
	ErrTooManyAttachments = errors.New("jumlah attachment sudah mencapai batas maksimal")
	ErrContentTypeBad     = errors.New("tipe konten tidak sesuai dengan yang diizinkan")
	ErrFileTooLarge       = errors.New("ukuran file melebihi batas maksimal")
	ErrUploadNotFound     = errors.New("file belum berhasil diupload ke storage")
	ErrInvalidKey         = errors.New("key tidak valid untuk note/kind ini")
)

type AttachmentService struct {
	q                     *sqlc.Queries
	storage               *storage.Client
	maxImageSize          int64
	maxVideoSize          int64
	maxAttachmentsPerNote int
}

func NewAttachmentService(q *sqlc.Queries, storage *storage.Client, maxImageSize, maxVideoSize int64, maxPerNote int) *AttachmentService {
	return &AttachmentService{
		q: q, storage: storage,
		maxImageSize: maxImageSize, maxVideoSize: maxVideoSize,
		maxAttachmentsPerNote: maxPerNote,
	}
}

func (s *AttachmentService) allowedTypesAndLimit(kind string) (map[string]bool, int64, error) {
	switch kind {
	case "image":
		return fileutil.AllowedImageTypes, s.maxImageSize, nil
	case "video":
		return fileutil.AllowedVideoTypes, s.maxVideoSize, nil
	default:
		return nil, 0, ErrInvalidKind
	}
}

// nextAttachmentIndex mengambil index attachment berikutnya untuk note tertentu.
func (s *AttachmentService) nextAttachmentIndex(ctx context.Context, noteID string) (int32, error) {
	return s.q.GetNextAttachmentIndex(ctx, noteID)
}

// checkCapacity memastikan note ada dan kuota attachment belum penuh.
// Mengembalikan note-nya untuk dipakai lebih lanjut. Tidak ada lagi konsep
// kepemilikan (owner) — siapapun yang punya akses ke note (via share link)
// bisa menambahkan attachment.
func (s *AttachmentService) checkCapacity(ctx context.Context, noteID string) (sqlc.Note, error) {
	n, err := s.q.GetNote(ctx, noteID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Note{}, ErrNotFound
		}
		return sqlc.Note{}, err
	}
	count, err := s.q.CountAttachmentsByNote(ctx, noteID)
	if err != nil {
		return sqlc.Note{}, err
	}
	if int(count) >= s.maxAttachmentsPerNote {
		return sqlc.Note{}, ErrTooManyAttachments
	}
	return n, nil
}

// ---------- FLOW PUBLIC (presigned URL langsung ke R2) ----------

func (s *AttachmentService) PresignUpload(ctx context.Context, noteID, contentType string, fileSize int64, kind string) (uploadURL, key string, err error) {
	n, err := s.checkCapacity(ctx, noteID)
	if err != nil {
		return "", "", err
	}
	if n.Mode == ModePrivate {
		return "", "", errors.New("note private memakai endpoint upload khusus (terenkripsi), bukan presign")
	}

	allowed, maxSize, err := s.allowedTypesAndLimit(kind)
	if err != nil {
		return "", "", err
	}
	if !allowed[contentType] {
		return "", "", ErrContentTypeBad
	}
	if fileSize <= 0 || fileSize > maxSize {
		return "", "", ErrFileTooLarge
	}

	rnd, err := storage.RandomHex(8)
	if err != nil {
		return "", "", err
	}
	ext := fileutil.ExtFromContentType(contentType)
	key = fmt.Sprintf("notes/%s/%ss/%d-%s.%s", noteID, kind, time.Now().Unix(), rnd, ext)

	uploadURL, err = s.storage.GeneratePresignedPutURL(ctx, key, contentType)
	return uploadURL, key, err
}

func (s *AttachmentService) ConfirmUpload(ctx context.Context, noteID, key, kind string) (sqlc.NoteAttachment, error) {
	n, err := s.checkCapacity(ctx, noteID)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	if n.Mode == ModePrivate {
		return sqlc.NoteAttachment{}, errors.New("gunakan endpoint upload private untuk note ini")
	}

	prefix := fmt.Sprintf("notes/%s/%ss/", noteID, kind)
	if len(key) < len(prefix) || key[:len(prefix)] != prefix {
		return sqlc.NoteAttachment{}, ErrInvalidKey
	}

	allowed, maxSize, err := s.allowedTypesAndLimit(kind)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}

	size, _, err := s.storage.HeadObject(ctx, key)
	if err != nil {
		return sqlc.NoteAttachment{}, ErrUploadNotFound
	}
	if size > maxSize {
		_ = s.storage.DeleteObject(ctx, key)
		return sqlc.NoteAttachment{}, ErrFileTooLarge
	}

	head, err := s.storage.GetObjectRange(ctx, key, 512)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	actualType, err := fileutil.DetectAndValidate(head, "", allowed)
	if err != nil {
		_ = s.storage.DeleteObject(ctx, key)
		return sqlc.NoteAttachment{}, err
	}

	index, err := s.nextAttachmentIndex(ctx, noteID)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}

	return s.q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
		NoteID:          noteID,
		AttachmentIndex: index,
		R2Key:           key,
		Url:             s.storage.PublicURL(key),
		ContentType:     actualType,
		FileSize:        size,
		Kind:            kind,
		Encrypted:       false,
	})
}

// ---------- FLOW PRIVATE (upload/download lewat backend, terenkripsi) ----------

// UploadPrivate menerima file mentah (sudah dibatasi ukurannya oleh handler
// via http.MaxBytesReader), memverifikasi password note, mengenkripsi file
// dengan key yang sama dengan content note, lalu upload ke R2.
func (s *AttachmentService) UploadPrivate(ctx context.Context, noteID, password, kind, claimedContentType string, fileBytes []byte) (sqlc.NoteAttachment, error) {
	n, err := s.checkCapacity(ctx, noteID)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	if n.Mode != ModePrivate {
		return sqlc.NoteAttachment{}, errors.New("note ini bukan mode private, gunakan endpoint presign biasa")
	}

	allowed, maxSize, err := s.allowedTypesAndLimit(kind)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	if int64(len(fileBytes)) > maxSize {
		return sqlc.NoteAttachment{}, ErrFileTooLarge
	}

	// Validasi magic bytes dari file ASLI (sebelum dienkripsi)
	head := fileBytes
	if len(head) > 512 {
		head = head[:512]
	}
	actualType, err := fileutil.DetectAndValidate(head, "", allowed)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	_ = claimedContentType // hanya untuk logging

	// Verifikasi password: coba dekripsi content note lama
	key := cryptoutil.DeriveKey(password, n.Salt)
	if _, err := cryptoutil.Decrypt(n.Content, key); err != nil {
		return sqlc.NoteAttachment{}, ErrWrongPassword
	}

	encrypted, err := cryptoutil.Encrypt(fileBytes, key)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}

	rnd, err := storage.RandomHex(8)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}
	r2Key := fmt.Sprintf("notes/%s/%ss/%d-%s.enc", noteID, kind, time.Now().Unix(), rnd)

	url, err := s.storage.PutObject(ctx, r2Key, "application/octet-stream", encrypted)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}

	index, err := s.nextAttachmentIndex(ctx, noteID)
	if err != nil {
		return sqlc.NoteAttachment{}, err
	}

	return s.q.CreateAttachment(ctx, sqlc.CreateAttachmentParams{
		NoteID:          noteID,
		AttachmentIndex: index,
		R2Key:           r2Key,
		Url:             url,
		ContentType:     actualType,
		FileSize:        int64(len(fileBytes)),
		Kind:            kind,
		Encrypted:       true,
	})
}

// DownloadPrivate mengambil attachment terenkripsi dari R2 lalu mendekripsi
// dengan password. Endpoint PUBLIK (mengikuti pola Unlock note) — siapapun
// yang tahu password note bisa download.
func (s *AttachmentService) DownloadPrivate(ctx context.Context, noteID string, attachmentIndex int32, password string) (data []byte, contentType string, err error) {
	att, err := s.q.GetAttachmentByNoteAndIndex(ctx, sqlc.GetAttachmentByNoteAndIndexParams{
		NoteID:          noteID,
		AttachmentIndex: attachmentIndex,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	if !att.Encrypted {
		return nil, "", errors.New("attachment ini tidak terenkripsi, akses langsung via url")
	}

	n, err := s.q.GetNote(ctx, att.NoteID)
	if err != nil {
		return nil, "", err
	}

	encKey := cryptoutil.DeriveKey(password, n.Salt)
	encryptedData, err := s.storage.GetObject(ctx, att.R2Key)
	if err != nil {
		return nil, "", err
	}

	plain, err := cryptoutil.Decrypt(encryptedData, encKey)
	if err != nil {
		return nil, "", ErrWrongPassword
	}

	return plain, att.ContentType, nil
}

func (s *AttachmentService) Delete(ctx context.Context, noteID string, attachmentIndex int32) error {
	att, err := s.q.GetAttachmentByNoteAndIndex(ctx, sqlc.GetAttachmentByNoteAndIndexParams{
		NoteID:          noteID,
		AttachmentIndex: attachmentIndex,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	_ = s.storage.DeleteObject(ctx, att.R2Key) // best-effort
	rows, err := s.q.DeleteAttachment(ctx, sqlc.DeleteAttachmentParams{
		NoteID:          noteID,
		AttachmentIndex: attachmentIndex,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
