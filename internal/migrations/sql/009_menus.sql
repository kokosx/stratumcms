CREATE TABLE menus (
    id TEXT PRIMARY KEY,
    handle TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE menu_items (
    id TEXT PRIMARY KEY,
    menu_id TEXT NOT NULL,
    parent_id TEXT,
    label TEXT NOT NULL,
    item_type TEXT NOT NULL CHECK (item_type IN ('entry', 'custom')),
    entry_id TEXT,
    url TEXT,
    position INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (menu_id) REFERENCES menus(id) ON DELETE CASCADE,
    FOREIGN KEY (parent_id) REFERENCES menu_items(id) ON DELETE CASCADE,
    FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
);
INSERT INTO menus (id, handle, name, created_at, updated_at) VALUES ('menu_primary', 'primary', 'Primary', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
