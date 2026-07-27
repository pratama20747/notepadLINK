-- name: CreateAttachment :one
INSERT INTO note_attachments (id, note_id, r2_key, url, content_type, file_size, kind, encrypted)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetAttachmentByID :one
SELECT * FROM note_attachments WHERE id = $1;

-- name: ListAttachmentsByNote :many
SELECT * FROM note_attachments WHERE note_id = $1 ORDER BY created_at ASC;

-- name: CountAttachmentsByNote :one
SELECT COUNT(*) FROM note_attachments WHERE note_id = $1;

-- name: DeleteAttachment :execrows
DELETE FROM note_attachments WHERE id = $1;

-- name: DeleteAttachmentsByNote :exec
DELETE FROM note_attachments WHERE note_id = $1;
