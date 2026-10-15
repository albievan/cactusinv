-- name: ListCustomers :many
SELECT * FROM customers
WHERE (sqlc.arg(include_inactive) OR active = TRUE)
  AND (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR code LIKE CONCAT('%', sqlc.arg(query), '%'))
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountCustomers :one
SELECT COUNT(*) FROM customers
WHERE (sqlc.arg(include_inactive) OR active = TRUE)
  AND (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR code LIKE CONCAT('%', sqlc.arg(query), '%'));

-- name: GetCustomer :one
SELECT * FROM customers WHERE id = ?;

-- name: InsertCustomer :exec
INSERT INTO customers (id, code, name, email, cc, attention, billing_address, shipping_name, shipping_address, phone,
    tax_number, currency_code, terms_kind, terms_value, bank_account_id, default_notes, active, version)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1);

-- name: UpdateCustomer :execrows
UPDATE customers SET code = ?, name = ?, email = ?, cc = ?, attention = ?, billing_address = ?, shipping_name = ?,
    shipping_address = ?, phone = ?, tax_number = ?, currency_code = ?, terms_kind = ?, terms_value = ?,
    bank_account_id = ?, default_notes = ?, active = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteCustomer :execrows
DELETE FROM customers WHERE id = ?;

-- name: ListProducts :many
SELECT * FROM products
WHERE (sqlc.arg(include_inactive) OR active = TRUE)
  AND (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR sku LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(currency) = '' OR EXISTS (
        SELECT 1 FROM product_prices pp WHERE pp.product_id = products.id AND pp.currency_code = sqlc.arg(currency)))
ORDER BY name, id
LIMIT ? OFFSET ?;

-- name: CountProducts :one
SELECT COUNT(*) FROM products
WHERE (sqlc.arg(include_inactive) OR active = TRUE)
  AND (sqlc.arg(query) = '' OR name LIKE CONCAT('%', sqlc.arg(query), '%') OR sku LIKE CONCAT('%', sqlc.arg(query), '%'))
  AND (sqlc.arg(currency) = '' OR EXISTS (
        SELECT 1 FROM product_prices pp WHERE pp.product_id = products.id AND pp.currency_code = sqlc.arg(currency)));

-- name: GetProduct :one
SELECT * FROM products WHERE id = ?;

-- name: ListProductsByIDs :many
SELECT * FROM products WHERE id IN (sqlc.slice(ids));

-- name: ListPricesByProductIDs :many
SELECT * FROM product_prices WHERE product_id IN (sqlc.slice(ids)) ORDER BY product_id, currency_code;

-- name: InsertProduct :exec
INSERT INTO products (id, sku, name, description, unit, active, version) VALUES (?, ?, ?, ?, ?, ?, 1);

-- name: UpdateProduct :execrows
UPDATE products SET sku = ?, name = ?, description = ?, unit = ?, active = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: DeleteProduct :execrows
DELETE FROM products WHERE id = ?;

-- name: DeleteProductPrices :exec
DELETE FROM product_prices WHERE product_id = ?;

-- name: InsertProductPrice :exec
INSERT INTO product_prices (product_id, currency_code, unit_price_minor) VALUES (?, ?, ?);
