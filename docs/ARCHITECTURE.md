# Architecture — cactusInv

This document expands `CLAUDE.md` §2. When the two disagree, `CLAUDE.md` wins and this file
must be fixed. Principles behind these choices: `docs/ENGINEERING_PRINCIPLES.md`.

## 1. Shape

```
                ┌────────────────────────── cactusinv (one Go binary) ──────────────────────────┐
 Phone/Desktop  │                                                                               │
 browser ──────►│  HTTP adapter (oapi-codegen strict server, /api/v1, embedded React app at /)  │
                │        │                                                                      │
                │        ▼                                                                      │
                │  app (use cases) ──► ports ◄── adapters: mariadb · smtp · pdf · vault · clock │
                │        ▲                                                                      │
                │  workers: schedules (1 min) · e-mail (15 s) · invoice export (30 s) · cleanup │
                └───────┬──────────────────┬──────────────────┬──────────────────┬──────────────┘
                        ▼                  ▼                  ▼                  ▼
                   MariaDB 11         SMTP server      invoices volume     /run/secrets
              (system of record)   (Mailpit locally)  (<year>/Inv_no_*.pdf) (files or Vault)
                        ▲
                 backup job (daily mariadb-dump → backups volume)
```

- **Modular monolith** (ADR-001): one module `github.com/albievan/cactusinv`, one binary,
  one image. HTTP API, background workers and the web UI ship together.
- **MariaDB 11** is the only stateful dependency (ADR-002).
- Several replicas may run at once: workers coordinate through row locks
  (`FOR UPDATE SKIP LOCKED`), not leader election.

## 2. Repository layout

```
cactusInv/
├── cmd/cactusinv/main.go          # composition root: config → Vault → DB → adapters → app → serve
├── internal/
│   ├── domain/                    # pure Go — money, currency, bank account, customer, product,
│   │                              #   invoice (+ lines, totals, status machine), payment terms,
│   │                              #   numbering, schedule (+ recurrence), email, errors.go
│   ├── app/                       # one use case per file + app_test fakes
│   ├── ports/                     # interfaces + ports/fakes (in-memory, for unit tests)
│   └── adapters/
│       ├── mariadb/               # repositories, TxManager, sqlc output in ./sqlc, queries/*.sql
│       ├── http/                  # generated server (api.gen.go), handlers, auth, problem+json, SPA
│       ├── smtp/                  # ports.Mailer over net/smtp (STARTTLS / implicit TLS)
│       ├── pdf/                   # ports.InvoiceRenderer — layout mirrors example/Inv_no_CD_1195_26.pdf
│       ├── filesecrets/           # ports.Secrets from /run/secrets files (default)
│       ├── vault/                 # ports.Secrets over Vault KV v2 HTTP API (optional)
│       ├── archive/               # ports.InvoiceArchive on the invoices volume
│       ├── argon2/ idgen/         # password hashing, UUIDv7
│       ├── clock/                 # system clock
│       └── worker/                # tickers: schedules, e-mail, invoice export, housekeeping
├── migrations/                    # goose SQL (embedded), 00001_init.sql …
├── api/openapi/cactusinv.yaml     # THE contract
├── web/                           # React + TS + Vite + Tailwind (mobile-first); dist embedded
├── deploy/backup/backup.sh        # scheduled mariadb-dump + restore
├── deploy/k8s/cactusinv.yaml      # k3s/k8s manifests (rendered from .env)
├── deploy.sh  docker-compose.yml  .env   # one-script deployment; .env = defaults
├── docs/                          # this file, ENGINEERING_PRINCIPLES.md, adr/
├── Dockerfile  Makefile  sqlc.yaml  .golangci.yml
├── PLAN.md  CLAUDE.md  README.md  LICENSE
└── example/                       # the reference invoice (local only, git-ignored: personal data)
```

**Dependency rule:** `domain ← app ← ports ← adapters ← cmd`, enforced by `depguard`.

## 3. Domain model

```
Currency (ZAR, USD, EUR …)
  │ 1
  │ *
BankAccount ── is_default (exactly one default per currency)
  ▲ 0..1 override
  │
Customer ──── currency (1) ─── payment terms ─── invoice code ("CD")
  │ 1
  │ *
Invoice ──── currency = customer's currency at creation (frozen)
  │           bank snapshot = customer override ?: currency default (frozen at issue)
  │ 1..*
InvoiceLine ── product (0..1) ── description / qty / unit price / disc% (snapshot)

Product ── ProductPrice (product, currency, unit price)  — a price list per currency
Schedule ── customer, recurrence, lines template, auto-send ──► generates Invoices
EmailMessage (outbox) ── invoice, recipients, send-at, attempts, status
```

### 3.1 Money (ADR-005)

`Money{Amount int64; Currency Code}` in minor units. Arithmetic refuses mixed currencies
(`ErrCurrencyMismatch`). `Currency.MinorUnits` (0, 2 or 3) drives parsing and formatting.

### 3.2 Multi-currency (ADR-003)

- A **customer** is billed in exactly one currency.
- A **bank account** belongs to exactly one currency; each currency has at most one
  *default* account. A customer may override with a specific account of the same
  currency.
- A **product** has 0..n prices, one per currency. Adding a product to an invoice in a
  currency with no price is allowed only with an explicit unit price; the schedule runner
  refuses (`ErrNoPriceForCurrency`) rather than guess.
- **No FX conversion.** Ever. Amounts on one invoice are all in that invoice's currency.

### 3.3 Invoice lifecycle

```
          issue (allocates number, freezes)        send (outbox)          mark paid
 draft ───────────────────────────────► issued ───────────────► sent ───────────────► paid
   │                                      │                       │
   └── delete                             └──────── void ◄────────┘
```

- `draft`: editable, no number.
- `issued`: number allocated from the gap-free sequence; content frozen; PDF renderable.
- `sent`: at least one e-mail delivered (`sent_at` set by the dispatcher).
  Re-sending is allowed and recorded.
- `paid` / `void`: terminal. Void keeps the number (gap-free) and the PDF watermarks VOID.
- Every transition writes an `invoice_events` audit row in the same transaction.

### 3.4 Line maths

```
gross  = round(quantity_milli × unit_price_minor / 1000)
disc   = round(gross × discount_bp / 10000)
amount = gross − disc
subtotal       = Σ gross        total_discount = Σ disc
vat            = round((subtotal − total_discount) × vat_rate_bp / 10000)
total          = subtotal − total_discount + vat
```

VAT rate: the business default (`business_profile.vat_rate_bp`, 0 by default) unless the
invoice or schedule sets its own. At 0 % nothing VAT-related is printed or shown; otherwise
the totals box shows `VAT n%` and the header shows `VAT No` when one is configured.
Outstanding or overdue invoices are never e-mailed automatically.

Rounding: half away from zero, per line. Reference invoice: `14 × 192.94 = 2701.16`,
`1 × 550.00 = 550.00`, subtotal `3251.16`, discount `0.00`, total `3251.16`.

### 3.5 Payment terms

| Kind | Value | Due date | Printed text |
|---|---|---|---|
| `on_receipt` | – | issue date | "Payment due on receipt" |
| `days_after` | N | issue + N days | "Payment due within N days" |
| `day_of_next_month` | D | D-th of the month after issue (clamped to month end) | "Payment due before 7th of the next month" |

### 3.6 Invoice numbering

Format template on the business profile, default `{code}/{seq}/{yy}`:
`{code}` customer invoice code (e.g. `CD`), `{seq}` the global sequence value, `{yy}` /
`{yyyy}` issue year. Sequence start is configurable (to continue from `1195`). Allocation:
`SELECT next_value … FOR UPDATE` → use → `UPDATE … +1`, inside the issue transaction.

### 3.7 Schedules and recurrence

```
Schedule { customer, name, frequency: once|weekly|monthly|quarterly|yearly,
           interval (every N), day_of_month (1–28 or 0 = last day), weekday,
           send_time (HH:MM), time_zone, start_date, end_date?,
           lines template, header fields, auto_send, email subject/body template,
           status: active|paused|ended, next_run_at (UTC) }
```

- `Recurrence.Next(after time.Time) time.Time` is pure and table-tested.
- **Runner** (every minute): claim due schedules (`next_run_at <= now`, `status=active`,
  `FOR UPDATE SKIP LOCKED`) → inside one transaction: insert `schedule_runs(schedule_id,
  period_start)` (UNIQUE — idempotency), build the invoice from the template using the
  customer's currency price list, **issue** it, and if `auto_send` enqueue the e-mail;
  advance `next_run_at`; end the schedule when past `end_date`.
- A failed run is recorded in `schedule_runs.error`, the schedule is not advanced past the
  failing period more than `max_attempts` times, and it surfaces in the UI.

### 3.8 E-mail outbox (ADR-004)

- `email_outbox` row: `invoice_id`, `to`, `cc`, `subject`, `body`, `send_at`,
  `status: pending|sending|sent|failed|cancelled`, `attempts`, `next_attempt_at`,
  `last_error`, `idempotency_key UNIQUE`.
- **Send now** = row with `send_at = now`. **Send later** = row with `send_at` in the
  future (cancellable until picked up). **Recurring** = row written by the runner.
- **Dispatcher** (every 15 s): claim rows (`status=pending AND next_attempt_at<=now`,
  `SKIP LOCKED`), mark `sending`, render the PDF, send via `ports.Mailer`, then mark
  `sent` + set invoice `sent` (or back to `pending` with backoff `1m·2^attempts`, capped
  at 6 h; `failed` after 8 attempts). Stuck `sending` rows older than 10 min are reclaimed.
- Subject/body are templates with placeholders `{{invoice_number}}`, `{{customer_name}}`,
  `{{total}}`, `{{due_date}}`, `{{business_name}}`; rendered with `text/template` on a
  restricted data struct.

## 4. Data (MariaDB 11)

Tables: `business_profile` (single row), `users`, `sessions`, `currencies`,
`bank_accounts`, `customers`, `products`, `product_prices`, `invoice_sequences`,
`invoices`, `invoice_lines`, `invoice_events`, `schedules`, `schedule_lines`,
`schedule_runs`, `email_outbox`, `idempotency_keys`.

- IDs: UUIDv7 stored as `CHAR(36)` (`ascii_bin`) — time-ordered, debuggable (ADR-002).
- Money: `BIGINT` minor units. Quantities: `BIGINT` milli-units. Rates: `INT` basis points.
- Timestamps: `DATETIME(6)` UTC. Business dates: `DATE`.
- One-default-bank-per-currency: `default_marker TINYINT AS (IF(is_default, 1, NULL)) STORED`
  + `UNIQUE(currency_code, default_marker)` (MariaDB refuses string-valued `IF()` in generated
  columns; `STORED` rather than `PERSISTENT` so sqlc can parse it).
- Optimistic locking: `version INT` on `customers`, `products`, `invoices`, `schedules`.

## 5. HTTP API (`/api/v1`)

| Area | Endpoints |
|---|---|
| Auth | `POST /auth/login`, `POST /auth/logout`, `GET /auth/me` |
| Settings | `GET/PUT /settings/business` |
| Currencies | `GET/POST /currencies`, `PUT /currencies/{code}` |
| Bank accounts | `GET/POST /bank-accounts`, `GET/PUT/DELETE /bank-accounts/{id}` |
| Customers | `GET/POST /customers`, `GET/PUT/DELETE /customers/{id}` |
| Products | `GET/POST /products` (`?q=` lookup, `?currency=`), `GET/PUT/DELETE /products/{id}` (prices inline) |
| Invoices | `GET/POST /invoices`, `GET/PUT/DELETE /invoices/{id}`, `POST /invoices/{id}/issue`, `/void`, `/mark-paid`, `/send` (`sendAt?`), `GET /invoices/{id}/pdf`, `GET /invoices/{id}/emails` |
| Schedules | `GET/POST /schedules`, `GET/PUT/DELETE /schedules/{id}`, `POST /schedules/{id}/pause`, `/resume`, `/run-now`, `GET /schedules/{id}/preview` (next N run dates) |
| Outbox | `GET /emails` (`?status=`), `POST /emails/{id}/cancel`, `/retry` |
| Dashboard | `GET /dashboard` (outstanding per currency, overdue, upcoming sends) |
| Ops | `GET /healthz`, `GET /readyz`, `GET /metrics` (not under `/api`) |

Errors: `application/problem+json` with `type` URIs `https://cactusinv/problems/<code>`.
Mutations require header `X-Requested-With: cactusinv` (CSRF) and accept `Idempotency-Key`.

## 6. Configuration & secrets

| Env var | Meaning | Default |
|---|---|---|
| `CACTUSINV_HTTP_ADDR` | listen address | `:8080` |
| `CACTUSINV_DB_HOST` / `_PORT` / `_NAME` / `_USER` | MariaDB location | – / `3306` / `cactusinv` / `cactusinv` |
| `CACTUSINV_SECRETS` | `file` or `vault` | `file` |
| `CACTUSINV_SECRETS_DIR` | directory of secret files | `/run/secrets` |
| `CACTUSINV_VAULT_ADDR` / `_TOKEN_FILE` / `_KV_PATH` | Vault (when `CACTUSINV_SECRETS=vault`) | – / `/run/secrets/vault_token` / `secret/data/cactusinv` |
| `CACTUSINV_INVOICES_DIR` | invoice archive (the invoices volume) | `/data/invoices` |
| `CACTUSINV_DEFAULT_ADMIN_USER` / `_PASSWORD` | created on first start when no user exists | `admin` / `admin` |
| `CACTUSINV_SMTP_HOST` / `_PORT` / `_USER` / `_FROM` / `_TLS` | SMTP | – / `587` / – / – / `starttls` |
| `CACTUSINV_WORKERS` | run schedule runner + dispatcher in this replica | `true` |
| `CACTUSINV_MIGRATE_ON_START` | apply migrations at start | `true` |
| `CACTUSINV_COOKIE_SECURE` | `Secure` cookie flag | `true` |

Secrets: `db_password` (required), `smtp_password` (optional) — files or Vault keys
(ADR-006). Session tokens are random and stored hashed, so no signing key exists.

## 7. Observability

- `slog` JSON; every request logs `request_id`, route, status, duration.
- Prometheus: `cactusinv_http_requests_total`, `…_http_request_duration_seconds`,
  `cactusinv_outbox_pending`, `cactusinv_outbox_oldest_pending_seconds`,
  `cactusinv_emails_sent_total{result}`, `cactusinv_schedule_runs_total{result}`.
- `/readyz` pings MariaDB and checks the invoices volume is writable.
- OpenTelemetry tracing is deferred (PLAN → Later): one process, no fan-out to trace.

## 8. Frontend

React 18 + TypeScript + Vite 5 + Tailwind 3, `openapi-fetch` client generated from the
contract. Feature folders under `web/src/features/{dashboard,invoices,customers,products,
schedules,settings,auth}`. Mobile layout: sticky top bar, bottom tab bar (Home, Invoices,
Customers, Products, More), full-screen forms with a sticky action bar. ≥ `md`: side nav,
tables. The production build is embedded in the binary and served with SPA fallback.

## 9. Statements, e-mail schedules, SMTP and the disclaimer

- **Statement** (`domain.NewStatement`): every open invoice of the customer in the customer's
  currency, oldest first, with aging buckets (current, 1–30, 31–60, 61–90, 90+ days past due).
  Rendered as PDF on demand and when a statement e-mail is delivered.
- **Schedules** have a `kind`: `invoice` (issue from product lines, optional auto-send) or
  `email` with a scope — `unsent` (open, never e-mailed), `outstanding` (all open) or
  `statement`. Runs are idempotent per period and invoice.
- **Outbox** messages have a `kind` (`invoice` | `statement`); statement e-mails belong to a
  customer and their PDF is rendered at send time.
- **SMTP**: a single `smtp_settings` row (Gmail preset until saved); the password is AES-256-GCM
  ciphertext keyed by the `app_key` secret. `smtp.Dynamic` reads the settings on every send
  and falls back to the deployment server from `.env`.
- **Disclaimer**: never copied onto invoices; `domain.Disclaimer(customer, business)` is
  resolved for each PDF render.
- **Lines** always reference a product; description and unit price come from it.

## 10. Deployment, backups and the invoice archive

| Concern | Docker Desktop / Engine | k3s / Kubernetes |
|---|---|---|
| Entry point | `./deploy.sh --install --dockerdesktop` | `./deploy.sh --install --k3s` / `--k8s` |
| Definition | `docker-compose.yml` + `.env` | `deploy/k8s/cactusinv.yaml` rendered from `.env` |
| Image | built by Compose | built for the node's arch; k3s: `k3s ctr images import`; k8s: pushed to `K8S_REGISTRY` |
| Database data | `${MARIADB_DATA_DIR}` bind mount | PVC `mariadb-data` |
| Invoice PDFs | `${INVOICES_DIR}` bind mount | PVC `invoices` |
| Backups | `backup` service at `BACKUP_TIMES` → `${BACKUP_DIR}` | CronJob `BACKUP_CRON` → PVC `backups` |
| Secrets | files in `${SECRETS_DIR}` (generated) | Secret `cactusinv-secrets` (generated in-cluster) |
| Remote | `--ssh-access user@host` copies the project and runs it there | `--ssh-access` runs `kubectl`/`k3s ctr` over SSH |
| Uninstall | `--uninstall` keeps data; `--delete-uninstall` removes it | same; delete removes the namespace |

ADR-007 (deployment, backups) and ADR-008 (invoice archive) record the reasoning.
