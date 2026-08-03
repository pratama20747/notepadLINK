// Package service berisi business logic notepad, terpisah dari HTTP layer
// (handler) dan data access layer (sqlc) agar mudah di-testing dan di-reuse.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"notepad-sharelink/internal/cryptoutil"
	"notepad-sharelink/internal/db/sqlc"
	"notepad-sharelink/internal/idgen"
)

// Mode notepad yang didukung.
const (
	ModePublic  = "public"
	ModePrivate = "private"
)

// Sentinel errors — di-map ke HTTP status code yang sesuai di handler layer.
var (
	ErrNotFound           = errors.New("note tidak ditemukan")
	ErrWrongPassword      = errors.New("password salah")
	ErrInvalidMode        = errors.New("mode tidak valid")
	ErrPasswordNeeded     = errors.New("password wajib diisi untuk note mode private")
	ErrTitleTooLong       = errors.New("judul terlalu panjang, maksimal 200 karakter")
	ErrEditPasswordNeeded = errors.New("edit_password wajib diisi untuk note view-only")
	ErrWrongEditPassword  = cryptoutil.ErrWrongEditPassword
)

// NoteSummary digunakan untuk response list notes ringan — tanpa content/salt.
type NoteSummary struct {
	ID        string    `json:"id"`
	Mode      string    `json:"mode"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NoteService merangkum seluruh use-case terkait notepad.
// Aplikasi ini tidak punya konsep akun/login — semua note diakses lewat
// share link (ID note itu sendiri adalah "kunci akses" untuk mode public;
// untuk mode private ditambah password).
type NoteService struct {
	q *sqlc.Queries
}

// NewNoteService membuat NoteService baru.
func NewNoteService(q *sqlc.Queries) *NoteService {
	return &NoteService{q: q}
}

// nextID mengambil counter global berikutnya dan meng-encode-nya jadi ID slug.
func (s *NoteService) nextID(ctx context.Context) (string, error) {
	counter, err := s.q.GetNextCounter(ctx)
	if err != nil {
		return "", err
	}
	return idgen.Encode(counter), nil
}

// CreateNote membuat note baru. isViewOnly + editPassword bersifat opsional
// dan independen dari mode (public/private): kalau isViewOnly true, editPassword
// wajib diisi dan di-hash pakai bcrypt, lalu disimpan di edit_password_hash.
// editPassword ini HANYA mengotorisasi update/delete — tidak dipakai untuk
// enkripsi apapun (beda dengan `password` mode private).
func (s *NoteService) CreateNote(ctx context.Context, mode, title, content, password string, isViewOnly bool, editPassword string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if mode != ModePublic && mode != ModePrivate {
		return "", ErrInvalidMode
	}
	if len(title) > 200 {
		return "", ErrTitleTooLong
	}

	id, err := s.nextID(ctx)
	if err != nil {
		return "", err
	}

	var (
		storedContent []byte
		salt          []byte
	)

	switch mode {
	case ModePublic:
		storedContent = []byte(content)
		salt = []byte{}

	case ModePrivate:
		if password == "" {
			return "", ErrPasswordNeeded
		}
		salt, err = cryptoutil.GenerateSalt()
		if err != nil {
			return "", err
		}
		key := cryptoutil.DeriveKey(password, salt)
		storedContent, err = cryptoutil.Encrypt([]byte(content), key)
		if err != nil {
			return "", err
		}
	}

	var editHash pgtype.Text
	if isViewOnly {
		if editPassword == "" {
			return "", ErrEditPasswordNeeded
		}
		h, err := cryptoutil.HashEditPassword(editPassword)
		if err != nil {
			return "", err
		}
		editHash = pgtype.Text{String: h, Valid: true}
	}

	_, err = s.q.CreateNote(ctx, sqlc.CreateNoteParams{
		ID:               id,
		Mode:             mode,
		Content:          storedContent,
		Salt:             salt,
		Title:            title,
		IsViewOnly:       isViewOnly,
		EditPasswordHash: editHash,
	})
	if err != nil {
		return "", err
	}

	return id, nil
}

// GetNoteMeta mengembalikan mode note, title, content (HANYA jika mode-nya
// public), dan daftar attachment. Untuk mode private, content tidak
// dikembalikan — klien harus memanggil UnlockPrivateNote dengan password yang
// benar. Title & attachments selalu dikembalikan untuk semua mode.
//
// Endpoint ini PUBLIK — tidak butuh login. Siapapun bisa akses lewat share link.
func (s *NoteService) GetNoteMeta(ctx context.Context, id string) (mode string, title string, content string, attachments []sqlc.NoteAttachment, err error) {
	if ctx.Err() != nil {
		return "", "", "", nil, ctx.Err()
	}
	n, err := s.q.GetNote(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", nil, ErrNotFound
		}
		return "", "", "", nil, err
	}

	attachments, err = s.q.ListAttachmentsByNote(ctx, id)
	if err != nil {
		return "", "", "", nil, err
	}

	if n.Mode == ModePublic {
		return n.Mode, n.Title, string(n.Content), attachments, nil
	}
	return n.Mode, n.Title, "", attachments, nil
}

// UnlockPrivateNote memverifikasi password dan mengembalikan title, content
// asli (hasil dekripsi content), dan daftar attachment jika password benar.
// Title langsung dikembalikan sebagai plaintext (tidak perlu didekripsi).
//
// Endpoint ini PUBLIK — tidak butuh login. Siapapun bisa unlock pakai password.
func (s *NoteService) UnlockPrivateNote(ctx context.Context, id, password string) (title string, content string, attachments []sqlc.NoteAttachment, err error) {
	if ctx.Err() != nil {
		return "", "", nil, ctx.Err()
	}
	n, err := s.q.GetNote(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", nil, ErrNotFound
		}
		return "", "", nil, err
	}

	if n.Mode != ModePrivate {
		return "", "", nil, ErrInvalidMode
	}

	key := cryptoutil.DeriveKey(password, n.Salt)
	plaintextContent, err := cryptoutil.Decrypt(n.Content, key)
	if err != nil {
		return "", "", nil, ErrWrongPassword
	}

	attachments, err = s.q.ListAttachmentsByNote(ctx, id)
	if err != nil {
		return "", "", nil, err
	}

	// Title plaintext, tidak perlu didekripsi
	return n.Title, string(plaintextContent), attachments, nil
}

func (s *NoteService) UpdateNote(ctx context.Context, id, title, content, password, editPassword string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(title) > 200 {
		return ErrTitleTooLong
	}

	n, err := s.q.GetNote(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if n.IsViewOnly {
		if editPassword == "" {
			return ErrEditPasswordNeeded
		}
		if !n.EditPasswordHash.Valid {
			return ErrWrongEditPassword // seharusnya tidak terjadi, tapi jaga-jaga
		}
		if err := cryptoutil.VerifyEditPassword(editPassword, n.EditPasswordHash.String); err != nil {
			return err
		}
	}

	var newContent []byte

	switch n.Mode {
	case ModePublic:
		newContent = []byte(content)

	case ModePrivate:
		if password == "" {
			return ErrPasswordNeeded
		}
		key := cryptoutil.DeriveKey(password, n.Salt)
		if _, err := cryptoutil.Decrypt(n.Content, key); err != nil {
			return ErrWrongPassword
		}
		newContent, err = cryptoutil.Encrypt([]byte(content), key)
		if err != nil {
			return err
		}
	}

	_, err = s.q.UpdateNoteContent(ctx, sqlc.UpdateNoteContentParams{
		ID:      id,
		Content: newContent,
		Title:   title,
	})
	return err
}

func (s *NoteService) DeleteNote(ctx context.Context, id, password, editPassword string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	n, err := s.q.GetNote(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	if n.IsViewOnly {
		if editPassword == "" {
			return ErrEditPasswordNeeded
		}
		if !n.EditPasswordHash.Valid {
			return ErrWrongEditPassword
		}
		if err := cryptoutil.VerifyEditPassword(editPassword, n.EditPasswordHash.String); err != nil {
			return err
		}
	}

	if n.Mode == ModePrivate {
		if password == "" {
			return ErrPasswordNeeded
		}
		key := cryptoutil.DeriveKey(password, n.Salt)
		if _, err := cryptoutil.Decrypt(n.Content, key); err != nil {
			return ErrWrongPassword
		}
	}

	if err := s.q.DeleteAttachmentsByNote(ctx, id); err != nil {
		return err
	}

	rows, err := s.q.DeleteNote(ctx, id)
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}

	return nil
}
