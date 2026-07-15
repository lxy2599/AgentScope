-- Project-related queries.

-- name: CreateProject :one
INSERT INTO projects (name, description)
VALUES ($1, $2)
RETURNING *;

-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjectsByUser :many
SELECT p.* FROM projects p
JOIN project_members pm ON pm.project_id = p.id
WHERE pm.user_id = $1
ORDER BY p.created_at DESC;

-- name: AddProjectMember :one
INSERT INTO project_members (project_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateProjectToken :one
INSERT INTO project_tokens (project_id, token_hash, token_prefix, scope, description)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTokenByHash :one
SELECT * FROM project_tokens WHERE token_hash = $1;

-- name: ListProjectTokens :many
SELECT * FROM project_tokens WHERE project_id = $1 ORDER BY created_at DESC;

-- name: GetProjectMember :one
SELECT * FROM project_members WHERE project_id = $1 AND user_id = $2;
