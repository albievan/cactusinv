-- name: ListInvoices :many
SELECT * FROM invoices
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(overdue) = FALSE OR (status IN ('issued', 'sent') AND due_date < sqlc.arg(today)))
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountInvoices :one
SELECT COUNT(*) FROM invoices
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(overdue) = FALSE OR (status IN ('issued', 'sent') AND due_date < sqlc.arg(today)));

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = ?;

-- name: ListInvoiceLines :many
SELECT * FROM invoice_lines WHERE invoice_id = ? ORDER BY position;

-- name: InsertInvoice :exec
INSERT INTO invoices (id, number, status, customer_id, currency_code, issue_date, due_date, terms_kind, terms_value,
    terms_text, sales_person, job, shipping_method, shipping_terms, delivery_date, bill_to_name, bill_to_address,
    bill_to_attention, ship_to_name, ship_to_address, notes, tax_rate_bp, subtotal_minor, discount_minor, tax_minor,
    total_minor, bank_name, bank_branch, bank_branch_code, bank_account_name, bank_account_number, bank_account_type,
    bank_swift_bic, bank_iban, schedule_id, issued_at, sent_at, paid_at, voided_at, void_reason, created_at,
    updated_at, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateInvoice :execrows
UPDATE invoices SET number = ?, status = ?, issue_date = ?, due_date = ?, terms_kind = ?, terms_value = ?,
    terms_text = ?, sales_person = ?, job = ?, shipping_method = ?, shipping_terms = ?, delivery_date = ?,
    bill_to_name = ?, bill_to_address = ?, bill_to_attention = ?, ship_to_name = ?, ship_to_address = ?, notes = ?,
    tax_rate_bp = ?, subtotal_minor = ?, discount_minor = ?, tax_minor = ?, total_minor = ?, bank_name = ?,
    bank_branch = ?, bank_branch_code = ?, bank_account_name = ?, bank_account_number = ?, bank_account_type = ?,
    bank_swift_bic = ?, bank_iban = ?, issued_at = ?, sent_at = ?, paid_at = ?, voided_at = ?, void_reason = ?,
    updated_at = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteInvoiceLines :exec
DELETE FROM invoice_lines WHERE invoice_id = ?;

-- name: InsertInvoiceLine :exec
INSERT INTO invoice_lines (invoice_id, position, product_id, description, quantity_milli, unit_price_minor,
    discount_bp, amount_minor)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteInvoice :execrows
DELETE FROM invoices WHERE id = ?;

-- name: InsertInvoiceEvent :exec
INSERT INTO invoice_events (id, invoice_id, type, detail, at) VALUES (?, ?, ?, ?, ?);

-- name: ListInvoiceEvents :many
SELECT * FROM invoice_events WHERE invoice_id = ? ORDER BY at, id;

-- name: InvoiceTotals :many
SELECT currency_code,
       CAST(COUNT(*) AS SIGNED) AS open_count,
       CAST(SUM(total_minor) AS SIGNED) AS outstanding,
       CAST(SUM(CASE WHEN due_date < sqlc.arg(today) THEN 1 ELSE 0 END) AS SIGNED) AS overdue_count,
       CAST(SUM(CASE WHEN due_date < sqlc.arg(today) THEN total_minor ELSE 0 END) AS SIGNED) AS overdue
FROM invoices
WHERE status IN ('issued', 'sent')
GROUP BY currency_code
ORDER BY currency_code;

-- name: CountInvoicesForCustomer :one
SELECT COUNT(*) FROM invoices WHERE customer_id = ?;

-- name: ListUnexportedInvoices :many
SELECT id, version FROM invoices
WHERE status <> 'draft' AND (exported_version IS NULL OR exported_version <> version)
ORDER BY updated_at
LIMIT ?;

-- name: MarkInvoiceExported :exec
UPDATE invoices SET exported_version = ? WHERE id = ?;
