-- +goose Up
-- The customer says they paid (M42): the invoice e-mail carries a link to a page where the
-- customer marks the invoice paid, with an optional note. It does not record a payment:
-- customer_paid_at flags it for the business to check and mark paid. Each e-mail gets its
-- own random token, stored here only as its SHA-256.
ALTER TABLE invoices
    ADD COLUMN customer_paid_at   DATETIME(6)   NULL AFTER void_reason,
    ADD COLUMN customer_paid_note VARCHAR(1000) NOT NULL DEFAULT '' AFTER customer_paid_at;

CREATE TABLE invoice_response_tokens (
    token_hash  CHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    invoice_id  CHAR(36)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    KEY invoice_response_tokens_invoice (invoice_id),
    CONSTRAINT invoice_response_tokens_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE invoice_response_tokens;
ALTER TABLE invoices DROP COLUMN customer_paid_note, DROP COLUMN customer_paid_at;
