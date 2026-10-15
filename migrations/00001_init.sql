-- +goose Up
-- cactusInv initial schema (MariaDB 11). See docs/ARCHITECTURE.md §4.

CREATE TABLE business_profile (
    id             TINYINT       NOT NULL PRIMARY KEY,
    name           VARCHAR(200)  NOT NULL,
    address_lines  TEXT          NOT NULL,
    phone          VARCHAR(100)  NOT NULL,
    email          VARCHAR(254)  NOT NULL,
    tax_number     VARCHAR(64)   NOT NULL,
    number_format  VARCHAR(64)   NOT NULL,
    time_zone      VARCHAR(64)   NOT NULL,
    default_notes  TEXT          NOT NULL,
    email_subject  VARCHAR(300)  NOT NULL,
    email_body     TEXT          NOT NULL,
    version        INT           NOT NULL,
    updated_at     DATETIME(6)   NOT NULL,
    CONSTRAINT business_profile_single_row CHECK (id = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE invoice_sequences (
    name        VARCHAR(32)  NOT NULL PRIMARY KEY,
    next_value  BIGINT       NOT NULL,
    CONSTRAINT invoice_sequences_positive CHECK (next_value > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO invoice_sequences (name, next_value) VALUES ('invoice', 1);

CREATE TABLE currencies (
    code         CHAR(3)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    name         VARCHAR(64)  NOT NULL,
    symbol       VARCHAR(8)   NOT NULL,
    minor_units  TINYINT      NOT NULL,
    active       BOOLEAN      NOT NULL,
    CONSTRAINT currencies_minor_units CHECK (minor_units BETWEEN 0 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE bank_accounts (
    id              CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    currency_code   CHAR(3)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    label           VARCHAR(80)   NOT NULL,
    bank_name       VARCHAR(120)  NOT NULL,
    branch          VARCHAR(120)  NOT NULL,
    branch_code     VARCHAR(32)   NOT NULL,
    account_name    VARCHAR(120)  NOT NULL,
    account_number  VARCHAR(64)   NOT NULL,
    account_type    VARCHAR(64)   NOT NULL,
    swift_bic       VARCHAR(16)   NOT NULL,
    iban            VARCHAR(40)   NOT NULL,
    is_default      BOOLEAN       NOT NULL,
    active          BOOLEAN       NOT NULL,
    version         INT           NOT NULL,
    -- At most one default account per currency (ADR-003): 1 for the default, NULL (which
    -- UNIQUE ignores) otherwise. MariaDB rejects string-valued IF() in generated columns.
    default_marker  TINYINT AS (IF(is_default, 1, NULL)) STORED,
    UNIQUE KEY bank_accounts_one_default (currency_code, default_marker),
    KEY bank_accounts_currency (currency_code),
    CONSTRAINT bank_accounts_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE customers (
    id                   CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    code                 VARCHAR(10)    CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name                 VARCHAR(200)   NOT NULL,
    email                VARCHAR(254)   NOT NULL,
    cc                   TEXT           NOT NULL,
    attention            VARCHAR(200)   NOT NULL,
    billing_address      VARCHAR(1000)  NOT NULL,
    shipping_name        VARCHAR(200)   NOT NULL,
    shipping_address     VARCHAR(1000)  NOT NULL,
    phone                VARCHAR(100)   NOT NULL,
    tax_number           VARCHAR(64)    NOT NULL,
    currency_code        CHAR(3)        CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    terms_kind           VARCHAR(20)    NOT NULL,
    terms_value          INT            NOT NULL,
    bank_account_id      CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    default_notes        TEXT           NOT NULL,
    active               BOOLEAN        NOT NULL,
    version              INT            NOT NULL,
    UNIQUE KEY customers_code (code),
    KEY customers_name (name),
    CONSTRAINT customers_terms_kind CHECK (terms_kind IN ('on_receipt', 'days_after', 'day_of_next_month')),
    CONSTRAINT customers_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code),
    CONSTRAINT customers_bank_fk FOREIGN KEY (bank_account_id) REFERENCES bank_accounts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE products (
    id           CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    sku          VARCHAR(64)    NOT NULL,
    name         VARCHAR(200)   NOT NULL,
    description  VARCHAR(1000)  NOT NULL,
    unit         VARCHAR(32)    NOT NULL,
    active       BOOLEAN        NOT NULL,
    version      INT            NOT NULL,
    UNIQUE KEY products_sku (sku),
    KEY products_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- A product's explicit price per currency; there is no FX conversion (ADR-003).
CREATE TABLE product_prices (
    product_id        CHAR(36)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    currency_code     CHAR(3)   CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    unit_price_minor  BIGINT    NOT NULL,
    PRIMARY KEY (product_id, currency_code),
    KEY product_prices_currency (currency_code),
    CONSTRAINT product_prices_non_negative CHECK (unit_price_minor >= 0),
    CONSTRAINT product_prices_product_fk FOREIGN KEY (product_id) REFERENCES products (id) ON DELETE CASCADE,
    CONSTRAINT product_prices_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE schedules (
    id               CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    customer_id      CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name             VARCHAR(120)  NOT NULL,
    status           VARCHAR(10)   NOT NULL,
    frequency        VARCHAR(10)   NOT NULL,
    interval_count   INT           NOT NULL,
    day_of_month     INT           NOT NULL,
    weekday          INT           NOT NULL,
    send_hour        INT           NOT NULL,
    send_minute      INT           NOT NULL,
    time_zone        VARCHAR(64)   NOT NULL,
    start_date       DATE          NOT NULL,
    end_date         DATE          NULL,
    sales_person     VARCHAR(120)  NOT NULL,
    job              VARCHAR(120)  NOT NULL,
    shipping_method  VARCHAR(120)  NOT NULL,
    shipping_terms   VARCHAR(120)  NOT NULL,
    notes            TEXT          NOT NULL,
    tax_rate_bp      INT           NOT NULL,
    auto_send        BOOLEAN       NOT NULL,
    email_subject    VARCHAR(300)  NOT NULL,
    email_body       TEXT          NOT NULL,
    next_occurrence  DATETIME(6)   NULL,
    next_run_at      DATETIME(6)   NULL,
    last_run_at      DATETIME(6)   NULL,
    failure_count    INT           NOT NULL,
    last_error       VARCHAR(1000) NOT NULL,
    created_at       DATETIME(6)   NOT NULL,
    updated_at       DATETIME(6)   NOT NULL,
    version          INT           NOT NULL,
    KEY schedules_due (status, next_run_at),
    KEY schedules_customer (customer_id),
    CONSTRAINT schedules_status CHECK (status IN ('active', 'paused', 'ended')),
    CONSTRAINT schedules_frequency CHECK (frequency IN ('once', 'weekly', 'monthly', 'quarterly', 'yearly')),
    CONSTRAINT schedules_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE schedule_lines (
    schedule_id       CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    position          INT            NOT NULL,
    product_id        CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    description       VARCHAR(2000)  NOT NULL,
    quantity_milli    BIGINT         NOT NULL,
    unit_price_minor  BIGINT         NULL,
    discount_bp       INT            NOT NULL,
    PRIMARY KEY (schedule_id, position),
    CONSTRAINT schedule_lines_schedule_fk FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE,
    CONSTRAINT schedule_lines_product_fk FOREIGN KEY (product_id) REFERENCES products (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE invoices (
    id                CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    number            VARCHAR(64)    NULL,
    status            VARCHAR(10)    NOT NULL,
    customer_id       CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    currency_code     CHAR(3)        CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    issue_date        DATE           NULL,
    due_date          DATE           NULL,
    terms_kind        VARCHAR(20)    NOT NULL,
    terms_value       INT            NOT NULL,
    terms_text        VARCHAR(200)   NOT NULL,
    sales_person      VARCHAR(120)   NOT NULL,
    job               VARCHAR(120)   NOT NULL,
    shipping_method   VARCHAR(120)   NOT NULL,
    shipping_terms    VARCHAR(120)   NOT NULL,
    delivery_date     DATE           NULL,
    bill_to_name      VARCHAR(200)   NOT NULL,
    bill_to_address   VARCHAR(1000)  NOT NULL,
    bill_to_attention VARCHAR(200)   NOT NULL,
    ship_to_name      VARCHAR(200)   NOT NULL,
    ship_to_address   VARCHAR(1000)  NOT NULL,
    notes             TEXT           NOT NULL,
    tax_rate_bp       INT            NOT NULL,
    subtotal_minor    BIGINT         NOT NULL,
    discount_minor    BIGINT         NOT NULL,
    tax_minor         BIGINT         NOT NULL,
    total_minor       BIGINT         NOT NULL,
    bank_name         VARCHAR(120)   NOT NULL,
    bank_branch       VARCHAR(120)   NOT NULL,
    bank_branch_code  VARCHAR(32)    NOT NULL,
    bank_account_name VARCHAR(120)   NOT NULL,
    bank_account_number VARCHAR(64)  NOT NULL,
    bank_account_type VARCHAR(64)    NOT NULL,
    bank_swift_bic    VARCHAR(16)    NOT NULL,
    bank_iban         VARCHAR(40)    NOT NULL,
    schedule_id       CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    issued_at         DATETIME(6)    NULL,
    sent_at           DATETIME(6)    NULL,
    paid_at           DATETIME(6)    NULL,
    voided_at         DATETIME(6)    NULL,
    void_reason       VARCHAR(500)   NOT NULL,
    created_at        DATETIME(6)    NOT NULL,
    updated_at        DATETIME(6)    NOT NULL,
    version           INT            NOT NULL,
    UNIQUE KEY invoices_number (number),
    KEY invoices_customer (customer_id, created_at),
    KEY invoices_status_due (status, due_date),
    KEY invoices_created (created_at),
    CONSTRAINT invoices_status CHECK (status IN ('draft', 'issued', 'sent', 'paid', 'void')),
    CONSTRAINT invoices_number_when_issued CHECK (status = 'draft' OR number IS NOT NULL),
    CONSTRAINT invoices_customer_fk FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT invoices_currency_fk FOREIGN KEY (currency_code) REFERENCES currencies (code),
    CONSTRAINT invoices_schedule_fk FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE invoice_lines (
    invoice_id        CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    position          INT            NOT NULL,
    product_id        CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NULL,
    description       VARCHAR(2000)  NOT NULL,
    quantity_milli    BIGINT         NOT NULL,
    unit_price_minor  BIGINT         NOT NULL,
    discount_bp       INT            NOT NULL,
    amount_minor      BIGINT         NOT NULL,
    PRIMARY KEY (invoice_id, position),
    CONSTRAINT invoice_lines_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Audit trail; never updated or deleted except with a draft invoice.
CREATE TABLE invoice_events (
    id          CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    invoice_id  CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    type        VARCHAR(32)    NOT NULL,
    detail      VARCHAR(1000)  NOT NULL,
    at          DATETIME(6)    NOT NULL,
    KEY invoice_events_invoice (invoice_id, at),
    CONSTRAINT invoice_events_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- One row per processed schedule period: the idempotency guarantee (ADR-004).
CREATE TABLE schedule_runs (
    schedule_id  CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    period       DATE         NOT NULL,
    invoice_id   CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    at           DATETIME(6)  NOT NULL,
    PRIMARY KEY (schedule_id, period),
    CONSTRAINT schedule_runs_schedule_fk FOREIGN KEY (schedule_id) REFERENCES schedules (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Transactional outbox for invoice e-mails (ADR-004).
CREATE TABLE email_outbox (
    id               CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    invoice_id       CHAR(36)       CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    to_addrs         TEXT           NOT NULL,
    cc_addrs         TEXT           NOT NULL,
    subject          VARCHAR(300)   NOT NULL,
    body             TEXT           NOT NULL,
    status           VARCHAR(10)    NOT NULL,
    attempts         INT            NOT NULL,
    send_at          DATETIME(6)    NOT NULL,
    next_attempt_at  DATETIME(6)    NOT NULL,
    last_error       VARCHAR(1000)  NOT NULL,
    sent_at          DATETIME(6)    NULL,
    idempotency_key  VARCHAR(200)   NULL,
    created_at       DATETIME(6)    NOT NULL,
    updated_at       DATETIME(6)    NOT NULL,
    UNIQUE KEY email_outbox_idempotency (idempotency_key),
    KEY email_outbox_due (status, next_attempt_at),
    KEY email_outbox_invoice (invoice_id),
    CONSTRAINT email_outbox_status CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
    CONSTRAINT email_outbox_invoice_fk FOREIGN KEY (invoice_id) REFERENCES invoices (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE users (
    id             CHAR(36)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    email          VARCHAR(254)  NOT NULL,
    password_hash  VARCHAR(255)  NOT NULL,
    created_at     DATETIME(6)   NOT NULL,
    UNIQUE KEY users_email (email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE sessions (
    token_hash  CHAR(64)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    user_id     CHAR(36)     CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expires_at  DATETIME(6)  NOT NULL,
    created_at  DATETIME(6)  NOT NULL,
    KEY sessions_expires (expires_at),
    CONSTRAINT sessions_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE idempotency_keys (
    idem_key     VARCHAR(200)  CHARACTER SET ascii COLLATE ascii_bin NOT NULL PRIMARY KEY,
    fingerprint  CHAR(64)      CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status       INT           NOT NULL,
    body         MEDIUMBLOB    NOT NULL,
    created_at   DATETIME(6)   NOT NULL,
    KEY idempotency_keys_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE email_outbox;
DROP TABLE schedule_runs;
DROP TABLE invoice_events;
DROP TABLE invoice_lines;
DROP TABLE invoices;
DROP TABLE schedule_lines;
DROP TABLE schedules;
DROP TABLE product_prices;
DROP TABLE products;
DROP TABLE customers;
DROP TABLE bank_accounts;
DROP TABLE currencies;
DROP TABLE invoice_sequences;
DROP TABLE business_profile;
