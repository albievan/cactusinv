-- +goose Up
-- A recurring-invoice workflow raises the invoice, then e-mails it this many days later
-- (0 = immediately).
ALTER TABLE schedules ADD COLUMN email_delay_days INT NOT NULL DEFAULT 0;
ALTER TABLE schedules ADD CONSTRAINT schedules_email_delay CHECK (email_delay_days BETWEEN 0 AND 90);

-- +goose Down
ALTER TABLE schedules DROP CONSTRAINT schedules_email_delay;
ALTER TABLE schedules DROP COLUMN email_delay_days;
