-- name: ListInvoices :many
SELECT * FROM invoices
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(overdue) = FALSE OR status = 'overdue' OR (status IN ('issued', 'sent') AND due_date < sqlc.arg(today)))
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?;

-- name: CountInvoices :one
SELECT COUNT(*) FROM invoices
WHERE (sqlc.arg(status) = '' OR status = sqlc.arg(status))
  AND (sqlc.arg(customer_id) = '' OR customer_id = sqlc.arg(customer_id))
  AND (sqlc.arg(query) = '' OR number LIKE CONCAT('%', sqlc.arg(query), '%') OR bill_to_name LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(overdue) = FALSE OR status = 'overdue' OR (status IN ('issued', 'sent') AND due_date < sqlc.arg(today)));

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id = ?;

-- name: ListInvoiceLines :many
SELECT * FROM invoice_lines WHERE invoice_id = ? ORDER BY position;

-- name: InsertInvoice :exec
INSERT INTO invoices (id, number, status, customer_id, currency_code, issue_date, due_date, terms_kind, terms_value,
    terms_text, sales_person, job, shipping_method, shipping_terms, delivery_date, bill_to_name, bill_to_address,
    bill_to_attention, ship_to_name, ship_to_address, notes, tax_rate_bp, subtotal_minor, discount_minor, tax_minor,
    total_minor, bank_name, bank_branch, bank_branch_code, bank_account_name, bank_account_number, bank_account_type,
    bank_swift_bic, bank_iban, bank_country, schedule_id, issued_at, sent_at, paid_at, overdue_at, voided_at,
    void_reason, created_at, updated_at, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
    ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateInvoice :execrows
UPDATE invoices SET number = ?, status = ?, issue_date = ?, due_date = ?, terms_kind = ?, terms_value = ?,
    terms_text = ?, sales_person = ?, job = ?, shipping_method = ?, shipping_terms = ?, delivery_date = ?,
    bill_to_name = ?, bill_to_address = ?, bill_to_attention = ?, ship_to_name = ?, ship_to_address = ?, notes = ?,
    tax_rate_bp = ?, subtotal_minor = ?, discount_minor = ?, tax_minor = ?, total_minor = ?, bank_name = ?,
    bank_branch = ?, bank_branch_code = ?, bank_account_name = ?, bank_account_number = ?, bank_account_type = ?,
    bank_swift_bic = ?, bank_iban = ?, bank_country = ?, issued_at = ?, sent_at = ?, paid_at = ?, overdue_at = ?,
    voided_at = ?, void_reason = ?,
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

-- name: InvoiceTotals :many
SELECT currency_code,
       CAST(COUNT(*) AS SIGNED) AS open_count,
       CAST(SUM(total_minor) AS SIGNED) AS outstanding,
       CAST(SUM(CASE WHEN status = 'overdue' OR due_date < sqlc.arg(today) THEN 1 ELSE 0 END) AS SIGNED) AS overdue_count,
       CAST(SUM(CASE WHEN status = 'overdue' OR due_date < sqlc.arg(today) THEN total_minor ELSE 0 END) AS SIGNED) AS overdue
FROM invoices
WHERE status IN ('issued', 'sent', 'overdue')
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

-- name: MonthlyInvoiced :many
SELECT currency_code, CAST(DATE_FORMAT(issue_date, '%Y-%m') AS CHAR(7)) AS month,
       CAST(SUM(total_minor) AS SIGNED) AS total, CAST(COUNT(*) AS SIGNED) AS count
FROM invoices
WHERE status IN ('issued', 'sent', 'overdue', 'paid') AND issue_date >= sqlc.arg(since)
GROUP BY currency_code, month
ORDER BY month, currency_code;

-- name: MonthlyPaid :many
SELECT currency_code, CAST(DATE_FORMAT(paid_at, '%Y-%m') AS CHAR(7)) AS month,
       CAST(SUM(total_minor) AS SIGNED) AS total, CAST(COUNT(*) AS SIGNED) AS count
FROM invoices
WHERE status = 'paid' AND paid_at >= sqlc.arg(since)
GROUP BY currency_code, month
ORDER BY month, currency_code;

-- name: TopDebtors :many
SELECT i.customer_id, c.name, i.currency_code,
       CAST(SUM(i.total_minor) AS SIGNED) AS outstanding, CAST(COUNT(*) AS SIGNED) AS open_count
FROM invoices i JOIN customers c ON c.id = i.customer_id
WHERE i.status IN ('issued', 'sent', 'overdue')
GROUP BY i.customer_id, c.name, i.currency_code
ORDER BY outstanding DESC
LIMIT ?;
