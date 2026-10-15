-- +goose Up
-- Business-wide default VAT rate (basis points, 0 = no VAT). A schedule's tax rate becomes
-- optional: NULL means "use the business default at run time".
ALTER TABLE business_profile ADD COLUMN vat_rate_bp INT NOT NULL DEFAULT 0;
ALTER TABLE business_profile ADD CONSTRAINT business_profile_vat CHECK (vat_rate_bp BETWEEN 0 AND 10000);
ALTER TABLE schedules MODIFY COLUMN tax_rate_bp INT NULL;

-- +goose Down
UPDATE schedules SET tax_rate_bp = 0 WHERE tax_rate_bp IS NULL;
ALTER TABLE schedules MODIFY COLUMN tax_rate_bp INT NOT NULL;
ALTER TABLE business_profile DROP CONSTRAINT business_profile_vat;
ALTER TABLE business_profile DROP COLUMN vat_rate_bp;
