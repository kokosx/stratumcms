CREATE TABLE site_presentation (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    active_theme TEXT NOT NULL DEFAULT 'starter',
    styles_json TEXT NOT NULL,
    custom_css TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);

INSERT INTO site_presentation (id, active_theme, styles_json, custom_css, version, updated_at)
VALUES (1, 'starter', '{}', '', 1, CURRENT_TIMESTAMP);
