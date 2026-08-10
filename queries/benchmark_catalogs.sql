-- Durable immutable benchmark catalog revisions and a mutable project head.

-- name: CreateBenchmarkCatalogRevision :one
INSERT INTO benchmark_catalog_revisions (project_id, revision, payload)
VALUES ($1, $2, $3)
ON CONFLICT (project_id, revision) DO NOTHING
RETURNING project_id, revision, payload, created_at;

-- name: GetBenchmarkCatalogRevision :one
SELECT project_id, revision, payload, created_at
FROM benchmark_catalog_revisions
WHERE project_id = $1
  AND revision = $2;

-- name: UpsertBenchmarkCatalogHead :one
INSERT INTO benchmark_catalog_heads (project_id, revision)
VALUES ($1, $2)
ON CONFLICT (project_id) DO UPDATE
SET revision = EXCLUDED.revision,
    updated_at = now()
RETURNING project_id, revision, updated_at;

-- name: GetCurrentBenchmarkCatalog :one
SELECT revision.payload
FROM benchmark_catalog_heads AS head
JOIN benchmark_catalog_revisions AS revision
  ON revision.project_id = head.project_id
 AND revision.revision = head.revision
WHERE head.project_id = $1;
