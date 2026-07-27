-- name: GetNextCounter :one
UPDATE id_counter SET counter = counter + 1, updated_at = now()
WHERE id = 'global'
RETURNING counter;
