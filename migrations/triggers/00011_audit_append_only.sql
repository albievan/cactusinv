-- +goose Up
-- The audit trail cannot be changed or deleted through SQL: any UPDATE or DELETE fails.
-- (Kept outside the sqlc schema directory: sqlc's parser does not understand triggers.)
CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log FOR EACH ROW
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_log is append-only: entries cannot be changed';
CREATE TRIGGER audit_log_no_delete BEFORE DELETE ON audit_log FOR EACH ROW
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit_log is append-only: entries cannot be deleted';

-- +goose Down
DROP TRIGGER audit_log_no_delete;
DROP TRIGGER audit_log_no_update;
