-- ============================================================
-- TABEL id_counter
-- ============================================================
-- Counter global untuk menghasilkan ID note/attachment sequential
-- (dipakai oleh internal/idgen, di-encode ke base62 5 karakter).
CREATE TABLE IF NOT EXISTS id_counter (
    id          VARCHAR(10) PRIMARY KEY DEFAULT 'global',
    counter     BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO id_counter (id, counter) VALUES ('global', 0)
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- TABEL notes
-- ============================================================
CREATE TABLE IF NOT EXISTS notes (
    id                 VARCHAR(21) PRIMARY KEY,
    mode               VARCHAR(10) NOT NULL CHECK (mode IN ('public', 'private')),
    content            BYTEA NOT NULL,
    salt               BYTEA NOT NULL DEFAULT '',
    title              TEXT NOT NULL DEFAULT '',
    is_view_only       BOOLEAN NOT NULL DEFAULT false, -- Ditambahkan NOT NULL
    edit_password_hash TEXT NULL,                      -- Hash bcrypt/argon2
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Menjamin jika view_only aktif, hash password WAJIB ada
    CONSTRAINT chk_view_only_password
        CHECK (is_view_only = false OR (is_view_only = true AND edit_password_hash IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_notes_created_at ON notes (created_at);

COMMENT ON TABLE notes IS 'Table for storing notes with public/private mode';
COMMENT ON COLUMN notes.mode IS 'public: plaintext, private: nonce||ciphertext AES-GCM';
COMMENT ON COLUMN notes.content IS 'plaintext for public, nonce||ciphertext for private';
COMMENT ON COLUMN notes.salt IS '16 byte random salt for private mode, empty for public';
COMMENT ON COLUMN notes.is_view_only IS 'true jika note diproteksi password untuk akses edit/delete';
COMMENT ON COLUMN notes.edit_password_hash IS 'Hash password (bcrypt/argon2) untuk otorisasi pengeditan';

-- ============================================================
-- TABEL note_attachments
-- ============================================================
CREATE TABLE IF NOT EXISTS note_attachments (
    id                VARCHAR(32) PRIMARY KEY,   -- di-generate di kode (hex 16 byte), bukan DB
    note_id           VARCHAR(21) NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    attachment_index  INT NOT NULL,
    r2_key            TEXT NOT NULL,
    url               TEXT NOT NULL,
    content_type      TEXT NOT NULL,
    file_size         BIGINT NOT NULL,
    kind              VARCHAR(10) NOT NULL CHECK (kind IN ('image', 'video')),
    encrypted         BOOLEAN NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_note_attachments_note_index UNIQUE (note_id, attachment_index)
);

CREATE INDEX IF NOT EXISTS idx_note_attachments_note_id ON note_attachments (note_id);
COMMENT ON COLUMN note_attachments.attachment_index IS 'Index sequential per note, dimulai dari 1 (lihat GetNextAttachmentIndex)';
COMMENT ON COLUMN note_attachments.encrypted IS 'true jika note pemiliknya mode private — content_type & file_size di sini merujuk ke file ASLI (sebelum dienkripsi), bukan blob terenkripsi di R2';
COMMENT ON COLUMN note_attachments.r2_key IS 'key object di R2. Untuk attachment private, isinya adalah nonce||ciphertext AES-GCM (application/octet-stream)';
