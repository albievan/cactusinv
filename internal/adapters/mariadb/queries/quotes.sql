-- name: ListQuotes :many
SELECT * FROM quotes
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(adhoc_customer_id) = '' OR adhoc_customer_id = sqlc.arg(adhoc_customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(expired) = FALSE OR (status IN ('draft', 'sent') AND valid_until < sqlc.arg(today)))
  AND (sqlc.arg(answered_by) = '' OR answered_by = sqlc.arg(answered_by))
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountQuotes :one
SELECT COUNT(*) FROM quotes
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(adhoc_customer_id) = '' OR adhoc_customer_id = sqlc.arg(adhoc_customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(expired) = FALSE OR (status IN ('draft', 'sent') AND valid_until < sqlc.arg(today)))
  AND (sqlc.arg(answered_by) = '' OR answered_by = sqlc.arg(answered_by));

-- name: GetQuote :one
SELECT * FROM quotes WHERE id = ?;

-- name: GetQuoteByInvoice :one
SELECT * FROM quotes WHERE invoice_id = ?;

-- name: InsertQuote :exec
INSERT INTO quotes (id, number, status, customer_id, adhoc_customer_id, currency_code, issue_date, valid_until,
    sales_person, reference, bill_to_name, bill_to_address, bill_to_attention, notes, terms, tax_rate_bp,
    subtotal_minor, discount_minor, tax_minor, total_minor, invoice_id, sent_at, accepted_at, declined_at,
    decline_reason, answered_by, renewed_from, converted_at, created_at, updated_at, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateQuote :execrows
UPDATE quotes SET status = ?, customer_id = ?, issue_date = ?, valid_until = ?, sales_person = ?, reference = ?,
    bill_to_name = ?, bill_to_address = ?, bill_to_attention = ?, notes = ?, terms = ?, tax_rate_bp = ?,
    subtotal_minor = ?, discount_minor = ?, tax_minor = ?, total_minor = ?, invoice_id = ?, sent_at = ?,
    accepted_at = ?, declined_at = ?, decline_reason = ?, answered_by = ?, converted_at = ?, updated_at = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteQuote :execrows
DELETE FROM quotes WHERE id = ?;

-- name: SetQuotesCustomer :exec
UPDATE quotes SET customer_id = ?, updated_at = ?, version = version + 1
WHERE adhoc_customer_id = ? AND customer_id IS NULL;

-- name: CountQuotesForAdhoc :one
SELECT COUNT(*) FROM quotes WHERE adhoc_customer_id = ?;

-- name: ListQuoteLines :many
SELECT * FROM quote_lines WHERE quote_id = ? ORDER BY position;

-- name: InsertQuoteLine :exec
INSERT INTO quote_lines (quote_id, position, product_id, description, quantity_milli, unit_price_minor, discount_bp, amount_minor)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteQuoteLines :exec
DELETE FROM quote_lines WHERE quote_id = ?;

-- name: ListAdhocCustomers :many
SELECT * FROM adhoc_customers
WHERE (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR email LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(include_converted) = TRUE OR customer_id IS NULL)
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountAdhocCustomers :one
SELECT COUNT(*) FROM adhoc_customers
WHERE (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR email LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(include_converted) = TRUE OR customer_id IS NULL);

-- name: GetAdhocCustomer :one
SELECT * FROM adhoc_customers WHERE id = ?;

-- name: InsertAdhocCustomer :exec
INSERT INTO adhoc_customers (id, name, email, attention, address, phone, tax_number, currency_code, customer_id,
    converted_at, created_at, updated_at, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateAdhocCustomer :execrows
UPDATE adhoc_customers SET name = ?, email = ?, attention = ?, address = ?, phone = ?, tax_number = ?,
    currency_code = ?, customer_id = ?, converted_at = ?, updated_at = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteAdhocCustomer :execrows
DELETE FROM adhoc_customers WHERE id = ?;

-- name: GetQuoteSettings :one
SELECT * FROM quote_settings WHERE id = 1;

-- name: InsertQuoteSettings :exec
INSERT INTO quote_settings (id, valid_days, number_format, email_subject, email_body, terms, public_url, version, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, 1, ?);

-- name: UpdateQuoteSettings :execrows
UPDATE quote_settings SET valid_days = ?, number_format = ?, email_subject = ?, email_body = ?, terms = ?,
    public_url = ?, version = version + 1, updated_at = ?
WHERE id = 1 AND version = ?;

-- name: LockQuoteSequence :one
SELECT next_value FROM invoice_sequences WHERE name = 'quote' FOR UPDATE;

-- name: PeekQuoteSequence :one
SELECT next_value FROM invoice_sequences WHERE name = 'quote';

-- name: SetQuoteSequence :exec
INSERT INTO invoice_sequences (name, next_value) VALUES ('quote', ?)
ON DUPLICATE KEY UPDATE next_value = VALUES(next_value);

-- name: InsertQuoteResponseToken :exec
INSERT INTO quote_response_tokens (token_hash, quote_id, created_at) VALUES (?, ?, ?);

-- name: GetQuoteIDByResponseToken :one
SELECT quote_id FROM quote_response_tokens WHERE token_hash = ?;
