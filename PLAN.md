# PLAN — cactusInv

A simple, automated, multi-currency invoicing system: a standalone application with a
mobile-first UI on MariaDB, deployable on Docker Desktop, k3s or Kubernetes with one script.
Principles: `docs/ENGINEERING_PRINCIPLES.md`. Structure: `docs/ARCHITECTURE.md`.

Status values: `todo | in_progress | done | blocked`. Update as you go.

---

## Goal (Albie, 2026-10-15)

> Plan a simple invoice system. Look at `./example` for an example invoice. Build an
> automated invoicing system where invoice e-mails can be scheduled to be sent to
> customers. Add products to a look-up and charge in multiple currencies — attach a bank
> account to a currency and attach a currency to the customer. Mobile first, hexagonal
> architecture, database must be MariaDB. Document the plan and engineering principles.

Added the same day:

> Deploy on Docker as an independent application. Schedule database backups. Export every
> invoice to a mapped invoices volume. Use `.env` for the default volume mappings. Default
> user `admin` / `admin`. Publish to `github.com/albievan/cactusinv` (public), licensed like
> SQLite (public domain). Deploy to Docker Desktop or k3s/k8s with one script:
> `deploy.sh --install|--uninstall|--delete-uninstall --dockerdesktop|--k3s|--k8s
> --ssh-access user@host`.

## What the example invoice tells us

The reference invoice (`example/Inv_no_CD_1195_26.pdf`) is kept locally and **not published**:
it contains real names, phone numbers and bank details. Code, tests and docs use fictitious
equivalents with the same layout and amounts.

| Element on the example | Where it lives in cactusInv |
|---|---|
| Header "Example Software Consulting", P.O. Box, phone | `business_profile` |
| `Inv. No. CD/1195/26` | numbering template `{code}/{seq}/{yy}`; customer code `CD`; global sequence continues from 1195 |
| `DATE 21 August 2026` | `invoices.issue_date` |
| To: name + postal address, Att: Jane | customer billing address + attention (snapshotted on invoice) |
| Ship To: physical address | customer shipping address (snapshotted) |
| Sales Person / Job / Shipping Method / Shipping Terms / Delivery Date | optional invoice header fields |
| Payment Terms "Payment due before 7th of the next month" / Due Date | payment terms kind `day_of_next_month`, value 7 → due date computed |
| Lines: Quantity · Description · Unit Price · Disc · Amount | `invoice_lines` (product look-up or free text) |
| Disclaimer paragraph | invoice notes, defaulted from customer / business profile |
| Bank: Capitecbank, Branch, Savings Acc, Bank Code | bank account attached to ZAR (the customer's currency), snapshotted at issue |
| TOTAL DISCOUNT · SUBTOTAL · TOTAL | computed totals (+ optional tax line when tax rate > 0) |
| Monthly subscription lines ("monthly subscription", "14 concurrent users") | a **monthly schedule** for Acme that issues and e-mails this invoice automatically |

## Scope

**In:** business profile; currencies; bank accounts per currency; customers with currency,
terms and invoice code; product look-up with per-currency prices; invoices (draft → issue
→ send → paid / void) with PDF matching the example layout; e-mail now / at a scheduled
time; recurring schedules that issue and e-mail invoices automatically; e-mail outbox view
with retry/cancel; dashboard; single-admin login; mobile-first web app; MariaDB; Vault;
Docker / k3s / k8s deployment with one script; scheduled backups; invoice PDF archive;
one quality gate.

**Out (Later):** online payments, FX conversion, credit notes as a separate document,
multi-tenant / multi-user roles, customer portal, OpenTelemetry tracing, Helm chart,
inbound payment reconciliation, user management UI.

---

## Milestones

### M0 — Foundations
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 0.1 | Plan + engineering principles + architecture + ADRs | done | docs reviewed against goal | `PLAN.md`, `docs/*`, `CLAUDE.md` |
| 0.2 | Go module, layout, Makefile, golangci (depguard), sqlc, gofumpt | done | `make check` runs (empty pkgs pass) | `go.mod`, `Makefile`, `.golangci.yml`, `sqlc.yaml` |

### M1 — Domain (pure Go)
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 1.1 | Money, Currency, rounding, quantity parsing/formatting | done | mixed-currency ops error; reference maths test | `internal/domain/money.go` |
| 1.2 | BankAccount, Customer, PaymentTerms (due date + text) | done | "7th of next month" from 21 Aug 2026 → 7 Sep 2026 | `bank.go`, `customer.go`, `terms.go` |
| 1.3 | Product + price list per currency | done | price lookup by currency, missing → `ErrNoPriceForCurrency` | `product.go` |
| 1.4 | Invoice aggregate, lines, totals, status machine, numbering template | done | reference invoice totals 3251.16; illegal transitions error | `invoice.go`, `numbering.go` |
| 1.5 | Schedule + recurrence (`Next`) | done | month-end, leap year, TZ/DST table tests | `schedule.go`, `recurrence.go` |
| 1.6 | Email message + template rendering + backoff | done | placeholders rendered; backoff capped | `email.go` |

### M2 — Persistence (MariaDB)
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 2.1 | Initial goose migration (all tables, FKs, checks, generated default-bank column) | done | up/down clean on fresh MariaDB 11 | `migrations/00001_init.sql` |
| 2.2 | sqlc queries + generated code | done | `make gen` reproducible | `internal/adapters/mariadb/queries`, `sqlc/` |
| 2.3 | Repositories + TxManager + sequence allocation + SKIP LOCKED claims | done | integration tests via testcontainers pass | `internal/adapters/mariadb/*.go` |

### M3 — Application (use cases)
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 3.1 | Currencies, bank accounts, business profile | done | one default per currency enforced | `internal/app/*` |
| 3.2 | Customers, products (look-up, price lists) | done | customer currency must exist & be active | |
| 3.3 | Invoices: create/update draft, issue, void, mark paid, delete draft | done | gap-free numbers; frozen after issue | |
| 3.4 | Send invoice now / at time; cancel / retry e-mail | done | idempotent; outbox row in same tx | |
| 3.5 | Schedules CRUD, pause/resume, preview, run-now; RunDueSchedules | done | one invoice per period even with 2 concurrent runners | |
| 3.6 | DispatchEmails (render PDF, send, backoff, reclaim stuck) | done | failure → retry with backoff → failed after 8 | |
| 3.7 | Auth: login/logout/session, bootstrap admin CLI | done | argon2id; session expiry | |
| 3.8 | Dashboard query | done | outstanding per currency, overdue count, upcoming sends | |

### M4 — Adapters
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 4.1 | OpenAPI contract + oapi-codegen strict server | done | contract lints; generated code in sync | `api/openapi/cactusinv.yaml` |
| 4.2 | HTTP handlers, problem+json, auth + CSRF middleware, idempotency, metrics, SPA serving | done | httptest suite green | `internal/adapters/http` |
| 4.3 | PDF renderer reproducing the example layout | done | text of PDF contains number, totals, bank | `internal/adapters/pdf` |
| 4.4 | SMTP mailer (STARTTLS/TLS, attachment) | done | tested against in-process fake SMTP server | `internal/adapters/smtp` |
| 4.5 | Vault KV v2 secrets adapter | done | tested with httptest Vault | `internal/adapters/vault` |
| 4.6 | Workers + composition root + graceful shutdown + `admin create` command | done | binary starts against compose stack | `cmd/cactusinv`, `internal/adapters/worker` |

### M5 — Web (mobile-first)
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 5.1 | Vite/React/TS/Tailwind scaffold, generated API client, auth, app shell (bottom tabs) | done | lint/typecheck/test/build green | `web/` |
| 5.2 | Settings: business profile, currencies, bank accounts | done | | |
| 5.3 | Customers & products (look-up with per-currency prices) | done | | |
| 5.4 | Invoices: list, editor with product look-up, issue, send now/later, PDF, mark paid, void | done | | |
| 5.5 | Schedules: editor, preview next dates, pause/resume/run-now; Outbox view; Dashboard | done | | |

### M6 — Packaging & docs
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 6.1 | Dockerfile (distroless, non-root, web embedded) | done | image builds | `Dockerfile` |
| 6.2 | Compose: MariaDB 11 + app + backup + Mailpit; `.env` volume mappings; file secrets | done | install → sign in as admin → send invoice visible in Mailpit (verified 2026-10-15) | `docker-compose.yml`, `.env` |
| 6.3 | README (run, configure, operate) | done | | `README.md` |
| 6.4 | Review pass; `make check` green; report | done | all gates green | |

### M7 — Standalone deployment, backups, archive (added 2026-10-15)
| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 7.1 | Default `admin`/`admin` on first start | done | created only when no user exists; warning logged | `app/auth.go`, `cmd/cactusinv` |
| 7.2 | Invoice PDF export to the invoices volume (re-export on change) | done | `data/invoices/2026/Inv_no_CD_1195_26.pdf` written (verified) | `app/export.go`, `adapters/archive`, `migrations/00002` |
| 7.3 | Scheduled database backups + restore | done | backup written to `data/backups` (verified); CronJob on k8s | `deploy/backup/backup.sh` |
| 7.4 | `deploy.sh` for Docker Desktop, k3s, k8s, local or over SSH | done | Docker install verified end to end; k8s manifests pass `kubectl --dry-run` | `deploy.sh`, `deploy/k8s` |
| 7.5 | File-based secrets (Docker/K8s) with Vault as option | done | unit tests | `adapters/filesecrets` |
| 7.6 | Public domain licence (SQLite-style); publish to GitHub | done | public repo `albievan/cactusinv` | `LICENSE` |

### M8 — VAT and e-mail policy (Albie, 2026-10-15)

> Do not automatically e-mail outstanding invoices. I want to be able to define the VAT
> percentage, but by default it must be 0, and if it is 0 do not show anything related to
> VAT on the invoice.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 8.1 | Business default VAT % (default 0), overridable per invoice and per schedule | done | new invoice takes the default; explicit 0 overrides; 15 % of 3251.16 = 487.67 | `migrations/00003_vat.sql`, domain, app, API, web |
| 8.2 | VAT hidden entirely at 0 % (PDF, UI); labelled "VAT" otherwise; "VAT No" only when set | done | PDF test asserts no "VAT" text on a 0 % invoice | `adapters/pdf` |
| 8.3 | No automatic e-mailing of outstanding/overdue invoices; schedule auto-send is off unless chosen | done | no reminder feature; editor defaults auto-send off | `web/src/features/schedules` |

### M9 — Product-only lines, statements, e-mail schedules, SMTP, new look (Albie, 2026-10-15)

> Invoice lines are searched from all products, no free text. A product has a currency and
> unit cost. Bank accounts per currency: sort/branch code, branch name, account number,
> SWIFT, bank name, country. Mark invoices paid/overdue/void and see the stage (draft,
> issued & emailed…). The disclaimer is pulled every time the PDF is created. Trigger an
> invoice e-mail manually or build a per-customer schedule e-mailing outstanding or
> not-yet-e-mailed invoices. Configure the SMTP server, Gmail by default. Customers in a
> table. Send statements. A more modern invoice. A login screen and app shell in the
> green/cream style with version and build visible. Everything readable; full-width grids.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 9.1 | Lines only from the product look-up (searchable grid over all products); server rejects free text | done | 422 on a line without productId; combobox test | `app/invoices.go`, `web/src/components/LineEditor.tsx` |
| 9.2 | Product form leads with currency + unit cost; ≥ 1 price required | done | 422 without a price | `domain/product.go`, `features/products` |
| 9.3 | Bank account country; relabelled fields; printed on PDF | done | migration 00004 | `domain/bank.go`, PDF |
| 9.4 | Overdue status (mark overdue); stage draft/issued/emailed/overdue/paid/void; stage tracker | done | domain + HTTP tests | `domain/invoice.go`, `InvoiceDetail.tsx` |
| 9.5 | Disclaimer resolved at every PDF render (customer's, else business's) | done | re-render after editing shows new text | `app/invoices.go`, `domain.Disclaimer` |
| 9.6 | Per-customer e-mail schedules: unsent, outstanding, statement | done | migration 00005; app tests | `app/schedules.go` |
| 9.7 | SMTP settings in the app (Gmail preset), password AES-256-GCM with app_key, test e-mail | done | migration 00006; app + adapter tests | `app/smtp.go`, `adapters/aesgcm`, `adapters/smtp/dynamic.go` |
| 9.8 | Customer statements: aging, PDF, e-mail now or on schedule | done | migration 00007; statement tests | `domain/statement.go`, `app/statements.go` |
| 9.9 | Modern PDF design; readable (wrapping payment card/info strip; overlap regression test) | done | `TestPaymentCardNeverOverlapsTotals` | `adapters/pdf` |
| 9.10 | Login and app shell restyle; version · build on login and sidebar; full-width tables | done | web tests; screenshots reviewed | `web/src/components` |

### M10 — Audit trail, compact actions, dashboard, raise-then-e-mail workflow (Albie, 2026-10-15)

> Compact buttons for send e-mail, view PDF, mark paid, mark overdue, void. Show who
> created/voided/paid/e-mailed each invoice; a full audit trail that cannot be deleted.
> A dashboard for the platform. Schedule a workflow that raises a predefined recurring
> invoice and then e-mails it.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 10.1 | Append-only, hash-chained `audit_log`; DB triggers reject UPDATE/DELETE; verify endpoint (ADR-009) | done | integration test proves UPDATE/DELETE fail; chain verifies under concurrency; tampering detected | `migrations/00008–00011`, `domain/audit.go`, `app/audit.go` |
| 10.2 | Actor on every action (user, Scheduler, E-mail dispatcher); invoice "who did what" and audit trail; Audit log page | done | app test of the full trail with actors | `app/*`, `adapters/http/middleware.go`, `web/src/features/audit` |
| 10.3 | Compact action buttons on invoice rows and page (send, PDF, paid, overdue, void) | done | web build; screenshots | `web/src/components/InvoiceActions.tsx` |
| 10.4 | Dashboard: stat tiles, 12-month invoiced vs paid chart (validated palette), top debtors, upcoming, activity | done | web test; screenshot | `app/dashboard.go`, `web/src/features/dashboard` |
| 10.5 | Recurring invoice workflow: raise, then e-mail immediately or N days later; timeline preview | done | app test: not sent before the delay, sent after | `migrations/00012`, `domain/schedule.go`, `Schedules.tsx` |

---

## Decisions

| Date | Decision | Why |
|---|---|---|
| 2026-10-15 | Modular monolith, one binary (ADR-001) | "Simple invoice system"; hexagonal layers without microservice overhead |
| 2026-10-15 | MariaDB 11 + goose + sqlc(mysql engine) + go-sql-driver/mysql (ADR-002) | MariaDB mandated; versioned migrations and type-checked queries |
| 2026-10-15 | Explicit price per currency, no FX (ADR-003) | Albie asked to "charge in multi currency"; FX on invoices is a source of disputes and stale rates |
| 2026-10-15 | E-mail via transactional outbox; scheduled send = future outbox row (ADR-004) | one reliable mechanism for now / later / recurring |
| 2026-10-15 | Money as int64 minor units (ADR-005) | exactness |
| 2026-10-15 | Secrets from files by default, Vault optional (ADR-006) | standalone Docker/k8s deployment needs no external secret store |
| 2026-10-15 | Global invoice sequence + per-customer code (`CD/1195/26`) | mirrors the example; sequence start configurable to continue from 1195 |
| 2026-10-15 | No OTel tracing, no NATS | single process — nothing to trace across; recorded under Later |
| 2026-10-15 | React 18, Vite 5, Tailwind 3 | mature, small bundle (71 KB gz), runs on Node 20 |
| 2026-10-15 | Default `admin`/`admin` user, created only when no user exists | requested; logged as a warning; README says to change it |
| 2026-10-15 | Repository/module `github.com/albievan/cactusinv` | the name requested (Albie, 2026-10-15) |
| 2026-10-15 | VAT: one business default (0 %), per-invoice/per-schedule override, invisible at 0 % | requested (M8) |
| 2026-10-15 | No automatic e-mails for outstanding/overdue invoices by default | requested (M8); sending is an explicit action or an opted-in per-customer e-mail schedule (M9) |
| 2026-10-15 | Disclaimer is not stored on the invoice; it is resolved at PDF render time | requested (M9): edits apply to every PDF produced afterwards, including re-renders of issued invoices |
| 2026-10-15 | SMTP password stored in the DB, AES-256-GCM encrypted with a generated app_key secret | in-app setup requested (M9); the key never leaves the secrets volume |
| 2026-10-15 | Audit trail append-only by DB triggers + SHA-256 hash chain (ADR-009) | "full audit trail that cannot be deleted"; triggers stop changes, the chain detects anything that bypasses them |
| 2026-10-15 | Version from VERSION file, build = git commit count, stamped at build time | requested (M9): visible on login and in the sidebar |
| 2026-10-15 | One deploy script, Compose for Docker, plain manifests for k3s/k8s (ADR-007) | requested; no Helm dependency |
| 2026-10-15 | Every invoice archived as PDF, re-exported on change (ADR-008) | requested "all invoices exported to a volume" |
| 2026-10-15 | Missed schedule periods are caught up one by one | an invoice is never silently skipped; each is dated its own period |
| 2026-10-15 | Toolchain pinned to go1.26.9 | go1.26.0 standard library has known vulnerabilities |

## Later

- Credit notes; partial payments; payment reminders for overdue invoices (would reuse the
  outbox + schedules).
- Customer portal / "view invoice" link.
- OpenTelemetry tracing; Helm chart; multiple users with roles.

## Open questions

- None outstanding. (Overdue reminders: decided **no** — M8. VAT: configurable, default 0 — M8.)
