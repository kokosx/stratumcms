CREATE TABLE content_types (
    id TEXT PRIMARY KEY,
    handle TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    schema_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE entries (
    id TEXT PRIMARY KEY,
    content_type_id TEXT NOT NULL,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('draft', 'scheduled', 'published', 'private', 'archived')),
    author_id TEXT NOT NULL,
    parent_id TEXT,
    published_revision_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    published_at TEXT,
    FOREIGN KEY (content_type_id) REFERENCES content_types(id),
    FOREIGN KEY (author_id) REFERENCES users(id),
    FOREIGN KEY (parent_id) REFERENCES entries(id),
    FOREIGN KEY (published_revision_id) REFERENCES revisions(id)
);

CREATE TABLE revisions (
    id TEXT PRIMARY KEY,
    entry_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    title TEXT NOT NULL,
    document_json TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE (entry_id, number),
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by) REFERENCES users(id)
);

CREATE TABLE routes (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    canonical INTEGER NOT NULL DEFAULT 1 CHECK (canonical IN (0, 1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX entries_content_type_updated_idx ON entries(content_type_id, updated_at DESC);
CREATE INDEX revisions_entry_number_idx ON revisions(entry_id, number DESC);

INSERT INTO content_types (id, handle, name, schema_json, created_at, updated_at)
VALUES
  ('content_type_page', 'page', 'Page', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP),
  ('content_type_post', 'post', 'Post', '{}', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
