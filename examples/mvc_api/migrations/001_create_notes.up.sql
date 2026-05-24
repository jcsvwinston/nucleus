CREATE TABLE IF NOT EXISTS notes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    title      TEXT    NOT NULL,
    body       TEXT    NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME
);

-- recommend: CREATE INDEX idx_notes_deleted_at ON notes(deleted_at);
-- Soft-delete queries filter on deleted_at IS NULL; an index on that column
-- significantly speeds up Index/Show/Update/Destroy at scale.
