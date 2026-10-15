-- +goose Up
-- Schedules either issue a recurring invoice ('invoice') or e-mail a customer's existing
-- invoices ('email': the unsent ones, or every outstanding one).
ALTER TABLE schedules ADD COLUMN kind VARCHAR(10) NOT NULL DEFAULT 'invoice';
ALTER TABLE schedules ADD COLUMN email_scope VARCHAR(12) NOT NULL DEFAULT '';
ALTER TABLE schedules ADD CONSTRAINT schedules_kind CHECK (kind IN ('invoice', 'email'));
ALTER TABLE schedules ADD CONSTRAINT schedules_email_scope CHECK (email_scope IN ('', 'unsent', 'outstanding'));

-- +goose Down
ALTER TABLE schedules DROP CONSTRAINT schedules_email_scope;
ALTER TABLE schedules DROP CONSTRAINT schedules_kind;
ALTER TABLE schedules DROP COLUMN email_scope;
ALTER TABLE schedules DROP COLUMN kind;
