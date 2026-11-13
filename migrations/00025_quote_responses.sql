-- +goose Up
-- Customer answers to quotes (M41): the quote e-mail carries Accept and Decline links; each
-- e-mail gets its own random token, stored here only as its SHA-256. answered_by says who
-- recorded the acceptance or decline (staff in the app, or the customer through a link).
-- renewed_from links a quote made from an expired one. public_url is the address the
-- links point at (empty = the address the quote was sent from in the browser).
ALTER TABLE quotes
    ADD COLUMN answered_by  VARCHAR(10) NOT NULL DEFAULT '' AFTER decline_reason,
    ADD COLUMN renewed_from CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER answered_by,
    ADD CONSTRAINT quotes_answered_by CHECK (answered_by IN ('', 'staff', 'customer')),
    ADD CONSTRAINT quotes_renewed_from_fk FOREIGN KEY (renewed_from) REFERENCES quotes (id) ON DELETE SET NULL;
UPDATE quotes SET answered_by = 'staff' WHERE status IN ('accepted', 'declined', 'converted') AND (accepted_at IS NOT NULL OR declined_at IS NOT NULL);

ALTER TABLE quote_settings ADD COLUMN public_url VARCHAR(300) NOT NULL DEFAULT '' AFTER terms;

CREATE TABLE quote_response_tokens (
    token_hash  CHAR(64)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    quote_id    CHAR(36)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at  DATETIME(6) NOT NULL,
    KEY quote_response_tokens_quote (quote_id),
    CONSTRAINT quote_response_tokens_quote_fk FOREIGN KEY (quote_id) REFERENCES quotes (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE quote_response_tokens;
ALTER TABLE quote_settings DROP COLUMN public_url;
ALTER TABLE quotes DROP FOREIGN KEY quotes_renewed_from_fk;
ALTER TABLE quotes DROP CONSTRAINT quotes_answered_by;
ALTER TABLE quotes DROP COLUMN renewed_from, DROP COLUMN answered_by;
