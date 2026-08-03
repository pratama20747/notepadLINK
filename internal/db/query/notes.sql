-- name: CreateNote :one
INSERT INTO notes (id, mode, content, salt, title, is_view_only, edit_password_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetNote :one
SELECT * FROM notes WHERE id = $1;

-- name: UpdateNoteContent :one
UPDATE notes
SET content = $2, title = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteNote :execrows
DELETE FROM notes WHERE id = $1;

-- name: CountNotes :one
SELECT COUNT(*) FROM notes;
