-- name: GetNextAttachmentIndex :one
SELECT COALESCE(MAX(attachment_index), 0) + 1
FROM note_attachments
WHERE note_id = $1;

-- name: CreateAttachment :one
INSERT INTO note_attachments (id, note_id, attachment_index, r2_key, url, content_type, file_size, kind, encrypted)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetAttachmentByNoteAndIndex :one
SELECT * FROM note_attachments
WHERE note_id = $1 AND attachment_index = $2;

-- name: ListAttachmentsByNote :many
SELECT * FROM note_attachments
WHERE note_id = $1
ORDER BY attachment_index ASC;

-- name: CountAttachmentsByNote :one
SELECT COUNT(*) FROM note_attachments WHERE note_id = $1;

-- name: DeleteAttachment :execrows
DELETE FROM note_attachments
WHERE note_id = $1 AND attachment_index = $2;

-- name: DeleteAttachmentsByNote :exec
DELETE FROM note_attachments WHERE note_id = $1;
