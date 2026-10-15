-- +goose Up
-- The outbox also carries statement e-mails, which belong to a customer, not an invoice.
ALTER TABLE email_outbox ADD COLUMN kind VARCHAR(10) NOT NULL DEFAULT 'invoice';
ALTER TABLE email_outbox ADD COLUMN customer_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL;
ALTER TABLE email_outbox MODIFY COLUMN invoice_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind CHECK (kind IN ('invoice', 'statement'));
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE;
CREATE INDEX email_outbox_customer ON email_outbox (customer_id);
ALTER TABLE schedules DROP CONSTRAINT schedules_email_scope;
ALTER TABLE schedules ADD CONSTRAINT schedules_email_scope CHECK (email_scope IN ('', 'unsent', 'outstanding', 'statement'));

-- +goose Down
ALTER TABLE schedules DROP CONSTRAINT schedules_email_scope;
UPDATE schedules SET email_scope = 'outstanding' WHERE email_scope = 'statement';
ALTER TABLE schedules ADD CONSTRAINT schedules_email_scope CHECK (email_scope IN ('', 'unsent', 'outstanding'));
DELETE FROM email_outbox WHERE kind = 'statement';
ALTER TABLE email_outbox DROP FOREIGN KEY email_outbox_customer_fk;
DROP INDEX email_outbox_customer ON email_outbox;
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind;
ALTER TABLE email_outbox MODIFY COLUMN invoice_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL;
ALTER TABLE email_outbox DROP COLUMN customer_id;
ALTER TABLE email_outbox DROP COLUMN kind;
