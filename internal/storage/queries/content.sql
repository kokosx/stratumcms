-- name: GetContentTypeByHandle :one
SELECT id, handle, name, schema_json, created_at, updated_at FROM content_types WHERE handle = ?;

-- name: GetEntry :one
SELECT id, content_type_id, title, slug, status, author_id, parent_id, published_revision_id, created_at, updated_at, published_at FROM entries WHERE id = ?;

-- name: GetEntryRoute :one
SELECT id, path, resource_type, resource_id, canonical, created_at, updated_at
FROM routes WHERE resource_type = 'entry' AND resource_id = ? AND canonical = 1;

-- name: ListEntriesByType :many
SELECT e.id, e.content_type_id, e.title, e.slug, e.status, e.author_id, e.parent_id, e.published_revision_id, e.created_at, e.updated_at, e.published_at,
       r.path, u.display_name
FROM entries e JOIN routes r ON r.resource_type = 'entry' AND r.resource_id = e.id AND r.canonical = 1
JOIN users u ON u.id = e.author_id
WHERE e.content_type_id = ? ORDER BY e.updated_at DESC;

-- name: CreateEntry :exec
INSERT INTO entries (id, content_type_id, title, slug, status, author_id, parent_id, published_revision_id, created_at, updated_at, published_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateEntry :exec
UPDATE entries SET title = ?, slug = ?, status = ?, updated_at = ?, published_at = ?, published_revision_id = ? WHERE id = ?;

-- name: NextRevisionNumber :one
SELECT COALESCE(MAX(number), 0) + 1 FROM revisions WHERE entry_id = ?;

-- name: CreateRevision :exec
INSERT INTO revisions (id, entry_id, number, title, document_json, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ListRevisions :many
SELECT r.id, r.entry_id, r.number, r.title, r.document_json, r.created_by, r.created_at, u.display_name
FROM revisions r JOIN users u ON u.id = r.created_by WHERE r.entry_id = ? ORDER BY r.number DESC;

-- name: CreateRoute :exec
INSERT INTO routes (id, path, resource_type, resource_id, canonical, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: UpdateEntryRoute :exec
UPDATE routes SET path = ?, updated_at = ? WHERE resource_type = 'entry' AND resource_id = ? AND canonical = 1;
