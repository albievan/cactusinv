-- +goose Up
-- Partial payments (M58): payments are recorded against an issued invoice; the invoice is
-- paid when they cover its total. amount_paid_minor is the sum of its payments, kept with
-- the invoice so lists, totals and statements read the balance without a join. Invoices
-- already paid get their payment in 00031 (Go: the day paid in the business's time zone).
ALTER TABLE invoices ADD COLUMN amount_paid_minor BIGINT NOT NULL DEFAULT 0 AFTER total_minor;

CREATE TABLE invoice_payments (
    id            CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    invoice_id    CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    amount_minor  BIGINT       NOT NULL,
    received_on   DATE         NOT NULL,
    reference     VARCHAR(200) NOT NULL DEFAULT '',
    created_at    DATETIME(6)  NOT NULL,
    KEY invoice_payments_invoice (invoice_id, received_on),
    KEY invoice_payments_received (received_on),
    CONSTRAINT invoice_payments_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE invoice_payments;
ALTER TABLE invoices DROP COLUMN amount_paid_minor;
