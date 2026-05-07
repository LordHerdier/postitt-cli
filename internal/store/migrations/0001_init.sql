-- 0001_init.sql
-- Initial schema for postitt.

CREATE TABLE IF NOT EXISTS commands (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    command       TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    bookmarked    INTEGER NOT NULL DEFAULT 0,
    use_count     INTEGER NOT NULL DEFAULT 0,
    last_used     INTEGER,                       -- unix epoch, nullable
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    auto_fillable INTEGER NOT NULL DEFAULT 0,    -- desc was blank, retry later
    UNIQUE(command)
);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS command_tags (
    command_id INTEGER NOT NULL,
    tag_id     INTEGER NOT NULL,
    PRIMARY KEY (command_id, tag_id),
    FOREIGN KEY (command_id) REFERENCES commands(id) ON DELETE CASCADE,
    FOREIGN KEY (tag_id)     REFERENCES tags(id)     ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_commands_sort
    ON commands(bookmarked DESC, use_count DESC, last_used DESC);

CREATE INDEX IF NOT EXISTS idx_command_tags_tag
    ON command_tags(tag_id);

-- FTS5 over command + description for fuzzy search outside of fzf
-- (fzf does its own matching; this is for `postitt ls --search "..."` etc.)
CREATE VIRTUAL TABLE IF NOT EXISTS commands_fts USING fts5(
    command, description, content='commands', content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS commands_ai AFTER INSERT ON commands BEGIN
    INSERT INTO commands_fts(rowid, command, description)
    VALUES (new.id, new.command, new.description);
END;

CREATE TRIGGER IF NOT EXISTS commands_ad AFTER DELETE ON commands BEGIN
    INSERT INTO commands_fts(commands_fts, rowid, command, description)
    VALUES ('delete', old.id, old.command, old.description);
END;

CREATE TRIGGER IF NOT EXISTS commands_au AFTER UPDATE ON commands BEGIN
    INSERT INTO commands_fts(commands_fts, rowid, command, description)
    VALUES ('delete', old.id, old.command, old.description);
    INSERT INTO commands_fts(rowid, command, description)
    VALUES (new.id, new.command, new.description);
END;
