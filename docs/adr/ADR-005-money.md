# ADR-005 — Money as int64 minor units

**Status:** accepted · **Date:** 2026-10-15

## Decision
`domain.Money{Amount int64, Currency CurrencyCode}`, amount in the currency's minor unit
(cents for ZAR/USD/EUR, none for JPY, mils for KWD/BHD). Quantities are `int64`
milli-units; discount and tax rates are `int` basis points. Rounding is half away from
zero, applied per line. Over the wire money is `{ "amount": "2701.16", "currency": "ZAR" }`
— a decimal **string**, never a JSON number, parsed with the currency's minor units.
In MariaDB money is `BIGINT`.

## Consequences
Exact arithmetic and no float drift; the reference invoice is reproduced to the cent and
kept as a regression test.
