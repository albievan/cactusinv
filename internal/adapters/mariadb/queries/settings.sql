-- name: GetBusiness :one
SELECT * FROM business_profile WHERE id = 1;

-- name: InsertBusiness :exec
INSERT INTO business_profile (id, name, address_lines, phone, email, tax_number, number_format, time_zone,
    default_notes, email_subject, email_body, version, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?);

-- name: UpdateBusiness :execrows
UPDATE business_profile SET name = ?, address_lines = ?, phone = ?, email = ?, tax_number = ?, number_format = ?,
    time_zone = ?, default_notes = ?, email_subject = ?, email_body = ?, version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;

-- name: LockSequence :one
SELECT next_value FROM invoice_sequences WHERE name = 'invoice' FOR UPDATE;

-- name: PeekSequence :one
SELECT next_value FROM invoice_sequences WHERE name = 'invoice';

-- name: SetSequence :exec
UPDATE invoice_sequences SET next_value = ? WHERE name = 'invoice';

-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY code;

-- name: GetCurrency :one
SELECT * FROM currencies WHERE code = ?;

-- name: InsertCurrency :exec
INSERT INTO currencies (code, name, symbol, minor_units, active) VALUES (?, ?, ?, ?, ?);

-- name: UpdateCurrency :execrows
UPDATE currencies SET name = ?, symbol = ?, active = ? WHERE code = ?;

-- name: ListBankAccounts :many
SELECT * FROM bank_accounts
WHERE (sqlc.arg(currency) = '' OR currency_code = sqlc.arg(currency))
ORDER BY currency_code, label, id;

-- name: GetBankAccount :one
SELECT * FROM bank_accounts WHERE id = ?;

-- name: InsertBankAccount :exec
INSERT INTO bank_accounts (id, currency_code, label, bank_name, branch, branch_code, account_name, account_number,
    account_type, swift_bic, iban, is_default, active, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateBankAccount :execrows
UPDATE bank_accounts SET label = ?, bank_name = ?, branch = ?, branch_code = ?, account_name = ?, account_number = ?,
    account_type = ?, swift_bic = ?, iban = ?, is_default = ?, active = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteBankAccount :execrows
DELETE FROM bank_accounts WHERE id = ?;

-- name: ClearDefaultBankAccount :exec
UPDATE bank_accounts SET is_default = FALSE, version = version + 1
WHERE currency_code = ? AND id <> ? AND is_default = TRUE;
