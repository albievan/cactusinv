-- +goose Up
-- Tracks which version of each invoice has been written to the invoice archive volume.
ALTER TABLE invoices ADD COLUMN exported_version INT NULL;
CREATE INDEX invoices_export ON invoices (status, exported_version);

-- +goose Down
DROP INDEX invoices_export ON invoices;
ALTER TABLE invoices DROP COLUMN exported_version;
