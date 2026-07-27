# Notepad Sharelink

MVP notepad yang bisa dibagikan lewat link, dengan dua mode:

- **Public** — tanpa password, siapapun yang punya link bisa lihat. Disimpan **plaintext**.
- **Private** — dibuat dengan password, isi disimpan **terenkripsi** (AES-256-GCM, key diturunkan dari password via Argon2id). Untuk lihat/edit/hapus wajib memasukkan password yang sama.

Tidak ada konsep akun/login — ID note (hasil dari share link) adalah satu-satunya "kunci akses" untuk mode public, dan password untuk mode private. Siapapun yang punya ID + password (kalau private) bisa lihat, ubah, atau hapus note tersebut.

Fitur tambahan:

- **Attachment** — lampiran gambar/video ke note. Note public pakai presigned URL langsung ke [Cloudflare R2](https://www.cloudflare.com/products/r2/); note private di-enkripsi server-side (AES-256-GCM, key sama dengan content note) sebelum diupload.
- **ID sequential**: ID note/attachment dibuat dari counter global (base62, 5 karakter), bukan random.

## Stack

- Go + [Gin](https://github.com/gin-gonic/gin) — HTTP framework
- [sqlc](https://sqlc.dev/) — generate kode Go type-safe dari SQL (pakai driver `pgx/v5`)
- [Neon](https://neon.tech) — Postgres serverless
- `golang.org/x/crypto` — argon2 (derive key enkripsi note private)
- [Cloudflare R2](https://www.cloudflare.com/products/r2/) — object storage S3-compatible
- [AWS SDK v2](https://aws.github.io/aws-sdk-go-v2/) — S3 client untuk R2
- `crypto/aes` (GCM) — enkripsi note & attachment private (standard library)
- `github.com/ulule/limiter/v3` — rate limiting middleware (create & unlock)

## Struktur folder

```
notepad-sharelink/
├── cmd/server/main.go          # entry point
├── internal/
│   ├── config/                  # load env var (DB, R2, limit file)
│   ├── cryptoutil/              # derive key + encrypt/decrypt (AES-GCM, Argon2id)
│   ├── fileutil/                # validasi magic bytes file
│   ├── idgen/                   # encode counter → ID slug (base62 5 karakter)
│   ├── storage/                 # Cloudflare R2 client (S3-compatible)
│   ├── db/
│   │   ├── migrations/          # schema SQL
│   │   ├── query/               # query SQL sumber untuk sqlc generate
│   │   └── sqlc/                # hasil generate sqlc
│   ├── service/                 # business logic (notes, attachment)
│   ├── handler/                 # HTTP handler (Gin)
│   ├── middleware/              # rate limiter, logger
│   └── router/                  # route registration
├── sqlc.yaml
├── go.mod
├── go.sum
├── docker-compose.yaml
└── .env.example
```

## Cara menjalankan

### 1. Siapkan database

**Opsi A: Neon (Serverless Postgres)**

1. Buat project di [neon.tech](https://neon.tech), catat connection string-nya.
2. Jalankan isi `internal/db/migrations/001_create_notes.sql` ke database.

**Opsi B: Docker (Lokal)**

```bash
docker-compose up -d
```

Lalu jalankan migrasi:

```bash
psql postgres://myuser:mypassword@localhost:5432/mydatabase -f internal/db/migrations/001_create_notes.sql
```

### 2. Install sqlc & generate kode

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
```

### 3. Set environment variable

```bash
cp .env.example .env
# lalu isi semua env var yang dibutuhkan (lihat .env.example)
```

### 4. Install dependency & jalankan

```bash
go mod tidy
go run ./cmd/server
```

Server jalan di `http://localhost:8080` (atau sesuai `PORT`).

## API Notes

Semua endpoint di bawah prefix `/api/notes` bersifat **publik** — tidak ada login/akun. ID note (dari share link) adalah kunci akses untuk mode public; mode private butuh ID + password.

### Create note

```bash
curl -X POST localhost:8080/api/notes \
  -H "Content-Type: application/json" \
  -d '{"mode":"public","title":"Catatan pertama","content":"Halo dunia"}'

# Mode private
curl -X POST localhost:8080/api/notes \
  -H "Content-Type: application/json" \
  -d '{"mode":"private","title":"Rahasia","content":"Rahasia banget","password":"secret123"}'
```

Response:
```json
{"id":"aB3xQ","share_url":"/n/aB3xQ","mode":"public","title":"Catatan tanpa judul"}
```

### Get note (share link)

```bash
curl localhost:8080/api/notes/aB3xQ
```

- Mode public → langsung dapat `title`, `content`, dan `attachments`.
- Mode private → `{"locked":true}` beserta `title` & `attachments`; harus unlock dulu untuk dapat `content`.

### Unlock note private

```bash
curl -X POST localhost:8080/api/notes/aB3xQ/unlock \
  -H "Content-Type: application/json" \
  -d '{"password":"secret123"}'
```

Response berisi `title`, `content`, dan `attachments`.

### Update note

```bash
curl -X PUT localhost:8080/api/notes/aB3xQ \
  -H "Content-Type: application/json" \
  -d '{"title":"Judul baru","content":"Isi baru","password":"secret123"}'
```

`password` wajib diisi untuk note mode private (diverifikasi dulu dengan mencoba dekripsi content lama).

### Delete note

```bash
curl -X DELETE localhost:8080/api/notes/aB3xQ \
  -H "Content-Type: application/json" \
  -d '{"password":"secret123"}'
```

Menghapus note beserta seluruh attachment-nya.

## Attachment (lampiran file)

Endpoint attachment di prefix `/api/notes`, semua **publik**:

| Method | Path | Deskripsi |
|---|---|---|
| GET | `/:id/attachments` | Daftar attachment note |
| POST | `/:id/attachments/presign` | Presigned URL upload (note public) |
| POST | `/:id/attachments/confirm` | Konfirmasi upload selesai (note public) |
| POST | `/:id/attachments/private` | Upload attachment private (multipart, terenkripsi) |
| POST | `/attachments/:attachmentId/download` | Download attachment private (butuh password) |
| DELETE | `/attachments/:attachmentId` | Hapus attachment |

### Flow Public (presigned URL)

```bash
# 1. Presign
curl -X POST localhost:8080/api/notes/<noteId>/attachments/presign \
  -H "Content-Type: application/json" \
  -d '{"content_type":"image/png","file_size":2000000,"kind":"image"}'

# 2. Client PUT langsung ke upload_url (tidak lewat backend)

# 3. Confirm
curl -X POST localhost:8080/api/notes/<noteId>/attachments/confirm \
  -H "Content-Type: application/json" \
  -d '{"key":"notes/<noteId>/images/...png","kind":"image"}'
```

### Flow Private (server-side encrypted)

```bash
# Upload
curl -X POST localhost:8080/api/notes/<noteId>/attachments/private \
  -F "file=@foto.jpg" \
  -F "kind=image" \
  -F "password=secret123"

# Download (publik — siapapun dengan password)
curl -X POST localhost:8080/api/notes/attachments/<attachmentId>/download \
  -H "Content-Type: application/json" \
  -d '{"password":"secret123"}' \
  --output foto.jpg
```

## Environment Variables

Lihat `.env.example` untuk daftar lengkap.

| Variable | Wajib | Keterangan |
|---|---|---|
| `DATABASE_URL` | ✅ | Connection string Neon atau PostgreSQL |
| `R2_ACCOUNT_ID` | Untuk fitur attachment | Account ID Cloudflare R2 |
| `R2_ACCESS_KEY_ID` | Untuk fitur attachment | S3-compatible access key |
| `R2_SECRET_ACCESS_KEY` | Untuk fitur attachment | S3-compatible secret key |
| `R2_BUCKET_NAME` | Untuk fitur attachment | Nama bucket R2 |
| `R2_PUBLIC_BASE_URL` | Untuk fitur attachment | Public base URL bucket |
| `MAX_IMAGE_ATTACHMENT_SIZE_MB` | ❌ | Default `10` |
| `MAX_VIDEO_ATTACHMENT_SIZE_MB` | ❌ | Default `50` |
| `MAX_ATTACHMENTS_PER_NOTE` | ❌ | Default `10` |
| `PRESIGN_TTL_MINUTES` | ❌ | Default `10` |
| `PORT` | ❌ | Default `8080` |
| `APP_ENV` | ❌ | Isi `production` untuk JSON logging & CORS allowlist |

> Kalau kredensial R2 (`R2_*`) tidak diisi, fitur attachment otomatis dinonaktifkan — note tetap berfungsi normal tanpa lampiran.

## Catatan desain & keamanan

- **Tidak ada akun/login**: ID note (share link) + password (untuk mode private) adalah satu-satunya kontrol akses. Siapapun yang memegangnya bisa lihat, ubah, atau hapus note — tidak ada validasi kepemilikan.
- **Verifikasi password note private** dilakukan dengan mencoba dekripsi konten pakai AES-GCM. Kalau password salah, authentication tag gagal → dianggap password salah.
- **Salt & key derivation (private note)**: tiap note private punya salt unik (16 byte), key diturunkan pakai Argon2id.
- **ID sequential**: dihasilkan dari counter global (tabel `id_counter`) yang di-encode ke base62 5 karakter — bukan random seperti sebelumnya.
- **Attachment private**: dienkripsi server-side (AES-256-GCM) dengan key yang sama dengan content note. Metadata tetap terbuka via List.
- **Validasi file**: magic bytes via `http.DetectContentType`, ukuran via R2 HeadObject.
- **Rate limiting**: create note & unlock note dilindungi rate limiter per IP (masing-masing 5/menit & 20/menit).
- **Structured logging**: `log/slog` (JSON di production, text di development).
- **Graceful shutdown**: server menunggu request selesai (timeout 10 detik).
- **CORS**: development allow all; production allowlist terbatas.
- **404 handler**: return JSON.
- **Ini MVP/prototype**: belum ada TTL/auto-expire note, dan karena tidak ada akun, siapapun yang tahu ID note bisa mengubah/menghapusnya (mode public) — pertimbangkan ini sebelum dipakai untuk data sensitif dalam skala besar.
