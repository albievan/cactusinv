-- +goose Up
-- The customer's account page (M59): a statement e-mail carries a link to a page, without
-- sign-in, with what the customer owes, their recent payments and every invoice and the
-- statement as PDFs. Each e-mail gets its own random token, stored here only as its
-- SHA-256, valid for expires_at (90 days).
CREATE TABLE customer_portal_tokens (
    token_hash   CHAR(64)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    customer_id  CHAR(36)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at   DATETIME(6) NOT NULL,
    expires_at   DATETIME(6) NOT NULL,
    KEY customer_portal_tokens_customer (customer_id),
    CONSTRAINT customer_portal_tokens_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE customer_portal_tokens;
