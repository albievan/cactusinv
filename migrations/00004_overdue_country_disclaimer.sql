-- +goose Up
-- Invoices can be marked overdue explicitly; bank accounts record their country.
ALTER TABLE invoices DROP CONSTRAINT invoices_status;
ALTER TABLE invoices ADD CONSTRAINT invoices_status CHECK (status IN ('draft', 'issued', 'sent', 'overdue', 'paid', 'void'));
ALTER TABLE invoices ADD COLUMN overdue_at DATETIME(6) NULL;
ALTER TABLE bank_accounts ADD COLUMN country VARCHAR(80) NOT NULL DEFAULT '';
ALTER TABLE invoices ADD COLUMN bank_country VARCHAR(80) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE invoices DROP COLUMN bank_country;
ALTER TABLE bank_accounts DROP COLUMN country;
ALTER TABLE invoices DROP COLUMN overdue_at;
UPDATE invoices SET status = 'sent' WHERE status = 'overdue';
ALTER TABLE invoices DROP CONSTRAINT invoices_status;
ALTER TABLE invoices ADD CONSTRAINT invoices_status CHECK (status IN ('draft', 'issued', 'sent', 'paid', 'void'));
