-- +goose Up
-- Quotes (M34): quotes priced from the products, for a customer or an ad-hoc customer (a
-- prospect), e-mailed through the outbox and converted into draft invoices. Quote numbers
-- have their own sequence; gaps are allowed (a quote is not a tax document).

-- Settings → Quotes (single row; defaults apply until first saved).
CREATE TABLE quote_settings (
    id             TINYINT       NOT NULL PRIMARY KEY,
    valid_days     INT           NOT NULL,
    number_format  VARCHAR(64)   NOT NULL,
    email_subject  VARCHAR(300)  NOT NULL,
    email_body     TEXT          NOT NULL,
    terms          TEXT          NOT NULL,
    version        INT           NOT NULL,
    updated_at     DATETIME(6)   NOT NULL,
    CONSTRAINT quote_settings_single_row CHECK (id = 1),
    CONSTRAINT quote_settings_valid_days CHECK (valid_days BETWEEN 1 AND 366)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO invoice_sequences (name, next_value) VALUES ('quote', 1);

-- A prospect a quote is made out to; customer_id is set when they become a customer.
CREATE TABLE adhoc_customers (
    id             CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    name           VARCHAR(200)   NOT NULL,
    email          VARCHAR(254)   NOT NULL,
    attention      VARCHAR(200)   NOT NULL,
    address        VARCHAR(1000)  NOT NULL,
    phone          VARCHAR(100)   NOT NULL,
    tax_number     VARCHAR(64)    NOT NULL,
    currency_code  CHAR(3)        CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    customer_id    CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    converted_at   DATETIME(6)    NULL,
    created_at     DATETIME(6)    NOT NULL,
    updated_at     DATETIME(6)    NOT NULL,
    version        INT            NOT NULL,
    KEY adhoc_customers_name (name),
    CONSTRAINT adhoc_customers_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code),
    CONSTRAINT adhoc_customers_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE quotes (
    id                 CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    number             VARCHAR(64)    NOT NULL,
    status             VARCHAR(10)    NOT NULL,
    customer_id        CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    adhoc_customer_id  CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    currency_code      CHAR(3)        CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    issue_date         DATE           NOT NULL,
    valid_until        DATE           NOT NULL,
    sales_person       VARCHAR(120)   NOT NULL,
    reference          VARCHAR(120)   NOT NULL,
    bill_to_name       VARCHAR(200)   NOT NULL,
    bill_to_address    VARCHAR(1000)  NOT NULL,
    bill_to_attention  VARCHAR(200)   NOT NULL,
    notes              MEDIUMTEXT     NOT NULL,
    terms              TEXT           NOT NULL,
    tax_rate_bp        INT            NOT NULL,
    subtotal_minor     BIGINT         NOT NULL,
    discount_minor     BIGINT         NOT NULL,
    tax_minor          BIGINT         NOT NULL,
    total_minor        BIGINT         NOT NULL,
    invoice_id         CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    sent_at            DATETIME(6)    NULL,
    accepted_at        DATETIME(6)    NULL,
    declined_at        DATETIME(6)    NULL,
    decline_reason     VARCHAR(500)   NOT NULL,
    converted_at       DATETIME(6)    NULL,
    created_at         DATETIME(6)    NOT NULL,
    updated_at         DATETIME(6)    NOT NULL,
    version            INT            NOT NULL,
    UNIQUE KEY quotes_number (number),
    KEY quotes_created (created_at),
    KEY quotes_customer (customer_id),
    KEY quotes_adhoc (adhoc_customer_id),
    CONSTRAINT quotes_status CHECK (status IN ('draft', 'sent', 'accepted', 'declined', 'converted')),
    CONSTRAINT quotes_for_someone CHECK (customer_id IS NOT NULL OR adhoc_customer_id IS NOT NULL),
    CONSTRAINT quotes_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT quotes_adhoc_fk FOREIGN KEY (adhoc_customer_id) REFERENCES adhoc_customers (id),
    CONSTRAINT quotes_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code),
    CONSTRAINT quotes_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE quote_lines (
    quote_id          CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    position          INT            NOT NULL,
    product_id        CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    description       VARCHAR(2000)  NOT NULL,
    quantity_milli    BIGINT         NOT NULL,
    unit_price_minor  BIGINT         NOT NULL,
    discount_bp       INT            NOT NULL,
    amount_minor      BIGINT         NOT NULL,
    PRIMARY KEY (quote_id, position),
    CONSTRAINT quote_lines_quote_fk FOREIGN KEY (quote_id) REFERENCES quotes (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Quote e-mails go through the same outbox.
ALTER TABLE email_outbox ADD COLUMN quote_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_quote_fk FOREIGN KEY (quote_id) REFERENCES quotes (id) ON DELETE CASCADE;
CREATE INDEX email_outbox_quote ON email_outbox (quote_id);
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind CHECK (kind IN ('invoice', 'statement', 'quote'));

-- +goose Down
DELETE FROM email_outbox WHERE kind = 'quote';
ALTER TABLE email_outbox DROP CONSTRAINT email_outbox_kind;
ALTER TABLE email_outbox ADD CONSTRAINT email_outbox_kind CHECK (kind IN ('invoice', 'statement'));
ALTER TABLE email_outbox DROP FOREIGN KEY email_outbox_quote_fk;
DROP INDEX email_outbox_quote ON email_outbox;
ALTER TABLE email_outbox DROP COLUMN quote_id;
DROP TABLE quote_lines;
DROP TABLE quotes;
DROP TABLE adhoc_customers;
DELETE FROM invoice_sequences WHERE name = 'quote';
DROP TABLE quote_settings;
