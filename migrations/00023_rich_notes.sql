-- +goose Up
-- Formatted invoice notes (M33): HTML from the notes editor, with pictures as data: URIs,
-- up to 800 kB (domain.NotesMaxBytes) — more than TEXT's 64 KiB. Existing plain notes are
-- kept as they are.
ALTER TABLE schedules MODIFY COLUMN notes MEDIUMTEXT NOT NULL;
ALTER TABLE invoices MODIFY COLUMN notes MEDIUMTEXT NOT NULL;

-- +goose Down
-- Formatted notes longer than TEXT allows must be shortened first; the statement refuses
-- (STRICT mode) rather than cut them.
ALTER TABLE invoices MODIFY COLUMN notes TEXT NOT NULL;
ALTER TABLE schedules MODIFY COLUMN notes TEXT NOT NULL;
