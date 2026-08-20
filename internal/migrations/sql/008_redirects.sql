CREATE TABLE redirects (
    id TEXT PRIMARY KEY,
    from_path TEXT NOT NULL UNIQUE,
    to_path TEXT NOT NULL,
    status_code INTEGER NOT NULL CHECK (status_code IN (301, 308)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
