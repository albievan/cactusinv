-- +goose Up
-- The id of the restore that set automation_held, written inside the swap transaction: it
-- tells whether a restore whose COMMIT reported an error went through (ADR-010).
ALTER TABLE system_state ADD COLUMN held_restore_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL;

-- +goose Down
ALTER TABLE system_state DROP COLUMN held_restore_id;
