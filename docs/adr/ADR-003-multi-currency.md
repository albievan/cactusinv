# ADR-003 — Multi-currency by explicit price lists, no FX

**Status:** accepted · **Date:** 2026-10-15

## Context
Albie wants to charge in multiple currencies, attach a bank account to a currency, and
attach a currency to each customer.

## Decision
- `Customer.Currency` is required. Every invoice for the customer is in that currency,
  frozen on the invoice at creation.
- `BankAccount.Currency` is required. Each currency may have one *default* account
  (enforced in domain and by a unique generated column). A customer may name an
  override account, which must be in the customer's currency.
- The bank details printed on an invoice are resolved at **issue** time
  (override → currency default → `ErrNoBankAccountForCurrency`) and snapshotted.
- Products carry a **price per currency**. Look-up returns the price in the requested
  currency. Manual line prices are always allowed on drafts; automatic schedule runs
  require a price list entry and fail loudly otherwise.
- There is **no exchange-rate conversion** anywhere in the system.

## Consequences
- Prices are deliberate business decisions per market, not a rate of the day.
- Reporting totals are per currency; the dashboard never sums across currencies.
- Changing a customer's currency affects future invoices only.
