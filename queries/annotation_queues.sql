-- Annotation queue queries.

-- name: CreateAnnotationQueue :one
INSERT INTO annotation_queues (project_id, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListAnnotationQueuesByProject :many
SELECT * FROM annotation_queues WHERE project_id = $1 ORDER BY created_at DESC;

-- name: GetAnnotationQueueByID :one
SELECT * FROM annotation_queues WHERE id = $1;

-- name: DeleteAnnotationQueue :one
DELETE FROM annotation_queues WHERE id = $1 AND project_id = $2 RETURNING id;

-- name: CreateAnnotationItem :one
INSERT INTO annotation_items (queue_id, project_id, run_id, trace_id, status, priority, scores,
                               corrected_output, expected_tool_path, comment,
                               agent_name, input_summary, output_summary, error,
                               failure_category, privacy_reviewed, reviewer_note)
SELECT q.id, q.project_id, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
FROM annotation_queues q
JOIN runs r ON r.id = $2 AND r.project_id = q.project_id
WHERE q.id = $1
RETURNING *;

-- name: ListAnnotationItemsByQueue :many
SELECT * FROM annotation_items WHERE queue_id = $1 ORDER BY priority DESC, created_at ASC;

-- name: GetAnnotationItemByID :one
SELECT * FROM annotation_items WHERE id = $1;

-- name: UpdateAnnotationItem :one
UPDATE annotation_items
SET status = $2,
    corrected_output = $3,
    expected_tool_path = $4,
    comment = $5,
    failure_category = $6,
    privacy_reviewed = $7,
    reviewer_note = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateAnnotationItemStatus :one
UPDATE annotation_items
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAnnotationItemsByProject :many
SELECT * FROM annotation_items WHERE project_id = $1 ORDER BY created_at DESC;
