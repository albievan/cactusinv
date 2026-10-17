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
one quality gate; user management (every user is an administrator); encrypted backup &
restore in the app (M14).

**Out (Later):** online payments, FX conversion, credit notes as a separate document,
multi-tenant / multi-user roles, customer portal, OpenTelemetry tracing, Helm chart,
inbound payment reconciliation.

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

### M11 — Statements menu (Albie, 2026-10-15)

> Add the statement menu and build out the statements for each of the customers.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 11.1 | `ListStatements`: every active customer's statement as of today (one pass over open invoices) with last statement e-mail; owing first | done | app test: ordering, balances, last e-mail | `app/statements.go` |
| 11.2 | `SendStatements` bulk e-mail with skip reasons (nothing owed, not found); `GET /statements`, `POST /statements/send`; outbox filter by customer | done | app + HTTP tests | `api/openapi/cactusinv.yaml`, `adapters/http/handlers_more.go` |
| 11.3 | Statements menu: per-currency totals, Owing/Overdue/All filters, search, aging columns, select and e-mail many | done | web test: filter, select, bulk send payload | `web/src/features/statements` |
| 11.4 | Full statement page per customer: balance, aging, unpaid invoices, e-mail history, PDF, e-mail, schedule | done | screenshot on a throwaway stack | `web/src/features/statements` |

### M12 — E-mail server setup guide (Albie, 2026-10-15)

> Add a help/info button to give you step by step instructions to setup the email server.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 12.1 | "Setup guide" button beside the E-mail server heading opens a sheet with numbered steps for Gmail, Outlook / Microsoft 365 and a custom server (the provider shown is the one selected, switchable in the sheet), and what to do when the test e-mail fails | done | web test: opens from the button, shows the selected provider's steps, switches provider, Escape and Close close it; `pnpm -C web check` and `make check` green | `web/src/features/settings/EmailSetupGuide.tsx`, `web/src/features/settings/Settings.tsx`, `web/src/components/icons.tsx` |

### M13 — Readable chips, visible disclaimer fallback, day in invoice numbers, Settings sub-menu (Albie, 2026-10-15) — v1.4.0

> The Outbox filter buttons are unreadable on dark-mode phones. When a customer has no
> disclaimer, use the business one. Invoice numbers like `{code}/{dd}{mm}-{seq}/{yy}`.
> Settings gets sub-items: Business, E-mail server, Currencies & bank accounts (Users and
> Backup & restore follow later).

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 13.1 | Light-only at the source: Tailwind `darkMode: 'class'` (the class is never set, so no `dark:` style can apply); every `dark:` class removed from `web/src`; unselected Outbox chip `bg-white text-brand-900 ring-1 ring-cream-300` | done | web tests: config has `darkMode: 'class'`; no `dark:` class anywhere in `web/src`; Outbox tabs carry readable classes | `web/tailwind.config.ts`, `web/src/features/emails/Outbox.tsx`, `web/src/features/settings/Settings.tsx`, `web/src/features/invoices/InvoiceDetail.tsx`, `web/src/lightOnly.test.ts`, `web/src/features/emails/Outbox.test.tsx` |
| 13.2 | Disclaimer fallback made visible: the API says which disclaimer a customer's PDFs use (`disclaimerSource`, from `domain.UsesOwnDisclaimer`); the customer form shows the business disclaimer as the textarea placeholder, the hint "Empty: the business disclaimer below is used", and on an existing customer which disclaimer its PDFs use. Every PDF path audited (invoice PDF, invoice e-mail, archive export, statement PDF, statement e-mail, scheduled invoice): all resolve through `domain.Disclaimer` | done | domain test of `UsesOwnDisclaimer`; app table test: a customer without a disclaimer gets the business disclaimer on every path; HTTP test of `disclaimerSource`; web test of placeholder, hint and source line | `api/openapi/cactusinv.yaml`, `internal/domain/invoice.go`, `internal/adapters/http/convert.go`, `internal/app/app_test.go`, `web/src/features/customers/Customers.tsx` |
| 13.3 | `{dd}` placeholder (issue day, 2 digits); `{code}/{dd}{mm}-{seq}/{yy}` is the default for new deployments; stored formats untouched; Settings hint, OpenAPI example, ARCHITECTURE, README | done | table tests incl. `CD/2108-1195/26` for 2026-08-21 seq 1195; unknown placeholder error lists `{dd}`; contracts in sync | `internal/domain/business.go`, `internal/domain/invoice_test.go`, `api/openapi/cactusinv.yaml`, `web/src/features/settings/Settings.tsx`, `docs/ARCHITECTURE.md`, `README.md` |
| 13.4 | Settings sub-items Business (`/settings/business`), E-mail server (`/settings/email`), Currencies & bank accounts (`/settings/currencies`); `/settings` → first; nested under Settings in the sidebar (expanded on a settings route, `aria-current`), listed on the phone More page; one `settingsNav` list ready for Users and Backup & restore (not built) | done | web tests: `/settings` redirects to Business; each route renders its section; sub-items show only on a settings route with `aria-current="page"` on the active one; More lists the three | `web/src/App.tsx`, `web/src/components/Layout.tsx`, `web/src/features/settings/Settings.tsx`, `web/src/features/settings/settingsNav.ts`, `web/src/App.test.tsx` |
| 13.5 | VERSION 1.4.0; `pnpm -C web check`; `make` fmt-check lint vet cover vuln contracts web-check | done | all green on macOS 2026-10-15 (domain 97.7 %, app 84.6 %); `make remote-test-int` green on the Docker test host (`ok internal/adapters/mariadb 50.978s`) | `VERSION`, `Makefile` (`remote-test-int`), `README.md` |

### M14 — Users, and backup & restore, under Settings (Albie, 2026-10-15)

> Add … user management as sub menu items under settings. Add the backup and restore under
> the setting menu item.

Depends on M13.4 (`settingsNav.ts` has slots for "Users" and "Backup & restore"). Part B is
built first (Albie's current goal), Part A follows. Migration numbers are the next free one
when a step starts (14.9 took `00013`; the 14.13 review fix took `00014_restore_outcome`, so Part A starts at `00015`). Planner findings that shaped it: the old
`backup.sh` writes unencrypted dumps (PII, bank details, password hashes); its restore
drops and recreates `audit_log` (triggers do not fire on DROP, bypassing ADR-009); the app
image is distroless (no `mariadb` client), so backups are an application-level export in
Go; API guards cap bodies at 1 MiB and the idempotency middleware buffers bodies, so the
backup routes need their own limits; the SMTP password is ciphertext under `app_key` and
does not decrypt on another installation.

#### Part A — User management

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 14.0 | ADR-010 (backup & restore), ADR-011 (users — written with Part A), ADR-009/007/006 amendments; Scope and Decisions | done | ADR files cross-referenced; make check green. ADR-011 (users): written with Part A (`docs/adr/ADR-011-users.md`; ADR-010 amended) | `docs/adr/ADR-010-backup-and-restore.md`, `docs/adr/ADR-011-users.md` (Part A), `docs/adr/ADR-00{6,7,9}-*.md`, `PLAN.md` |
| 14.1 | Domain user rules: `User` gains `DisabledAt`, `MustChangePassword`, `LastLoginAt`; `ValidateNewLogin` (one valid e-mail ≤ 254), `ValidatePassword` (12–200, moved from app), `CheckCanDisable(actorID, target, activeCount)` (never yourself, never the last active user); sentinels `ErrForbidden`, `ErrLastActiveUser`, `ErrPasswordChangeRequired` | done | table tests (self-disable refused, last active refused, re-enable allowed, e-mail validation, case-insensitive logins); domain ≥ 80 % | `internal/domain/user.go` (User/Session moved from `email.go`), `user_test.go`, `errors.go` |
| 14.2 | Migration `user_management` (`disabled_at`, `must_change_password`, `last_login_at`; Down works); sqlc; ports `UserRepository.{List, SetDisabled, SetMustChange, TouchLogin, LockActive}` (`LockActive` = `SELECT id … WHERE disabled_at IS NULL FOR UPDATE`), `SessionRepository.DeleteForUser(userID, exceptHash)`; fakes; MariaDB | done | `make remote-test-int`: every new method; up/down/up; two concurrent "disable the other admin" with two active users → exactly one succeeds | `migrations/`, `queries/auth.sql`, `sqlcgen/`, `settings.go`, `ports.go`, fakes |
| 14.3 | Use cases `internal/app/users.go`: `ListUsers`, `AddUser`, `ResetUserPassword` (must-change, revoke target sessions, refuse yourself), `DisableUser`/`EnableUser` (inside `LockActive`, revoke sessions); re-entering the actor's password guards add/reset/disable (`user.reauth_failed` audited); Login: disabled → generic failure + audited reason, sets `last_login_at`; `Authenticate` refuses disabled; `ChangePassword` clears must-change and revokes other sessions; `SessionAllows(user, operationID)`: a must-change session reaches only me/password/logout; `EnsureDefaultAdmin` sets must-change; audit `user.created/.password_reset/.disabled/.enabled/.password_changed` | done | table tests for every rule and audit entry; app ≥ 80 % | `internal/app/users.go`, `auth.go`, `app_test.go` |
| 14.4 | Contract: `GET/POST /users`, `POST /users/{id}/reset-password`, `/disable`, `/enable`; `Me.mustChangePassword` and `Me.automationPaused`; schemas `User`, `NewUserRequest{email,password,currentPassword}`, `ResetPasswordRequest{password,currentPassword}`, `ReauthRequest{currentPassword}`; problems `forbidden`, `last-active-user`, `password-change-required`; `make gen` | done | `make contracts` in sync | `api/openapi/cactusinv.yaml`, generated |
| 14.5 | Handlers; must-change gate in `withAPIGuards` (`app.SessionAllows`); `loginLimiter` also on re-auth endpoints; problem mapping | done | httptest per endpoint; must-change session 403 on `/invoices`, 204 on `/auth/password` | `internal/adapters/http/*` |
| 14.6 | Web Settings → Users (cards on phones, table ≥ md; Add user sheet; Reset password; Disable/Enable with confirmation; "You" badge; own password change; forced change-password screen when `me.mustChangePassword`) | done | Vitest; `pnpm -C web check` | `web/src/features/settings/users/*`, `App.tsx`, `settingsNav.ts` |
| 14.7 | README, ARCHITECTURE; throwaway stack check | done | make check green — done: README (Users, quick start, upgrade note), ARCHITECTURE §4/§5/§8/§13, ADR-011, VERSION 1.6.0; throwaway stack on the Docker host (v1.5.0 → this tree): default admin forced to change and continues, CLI e-mail user not flagged, add user → forced change, disable → session ends and sign-in refused (reason audited), self-disable 403, concurrent mutual disables ×4 → exactly one 200 and one 409 each round, audit verifies, `admin set-password` recovers; forced-change screen and Users checked in headless Chrome at 360 px (no horizontal overflow) and 1280 px; torn down and deleted | `README.md`, `docs/ARCHITECTURE.md` |

#### Part B — Backup & restore (ADR-010)

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 14.8 | Domain: `BackupHeader{FormatVersion=1, SchemaVersion, AppVersion, Build, CreatedAt, CreatedBy, Tables[{Name, Columns}]}`, `BackupDigest{Tables[{Name, Rows, SHA256}], AuditHead{Seq, Hash}}`; `CheckRestorable(header, currentSchema)` (newer → `ErrBackupTooNew`, unknown format → `ErrBackupUnsupported`); `BackupName`/`ParseBackupName` (`cactusinv-20261015T020000Z-s14-{scheduled,manual,upload}.age`, strict regex; stored names never from user input); `PlanAuditMerge(current, backup []AuditRef)` → `KeepCurrent` \| `AppendVerbatim(fromSeq)` \| `AppendResealed(fromSeq)`; `ParseBackupTimes`/`NextBackupSlot`; `ValidateBackupPassphrase` (≥ 16); sentinels `ErrBackupInvalid` (corrupt/tampered/wrong passphrase — one error), `ErrBackupTooNew`, `ErrBackupUnsupported`, `ErrBusy` | done | table tests; fuzz `ParseBackupName` and header/digest JSON; property: random chains with random fork points → merged chain verifies with `VerifyAuditChain`, keeps every current entry and every backup entry's content; slot maths across DST and midnight | `internal/domain/backup.go`, tests, `errors.go` |
| 14.9 | Migration `backup_state`: `system_state` (single row: `automation_held`, `held_at`, `held_by`, `held_reason`, staged-restore id, expiry, source name, sha256, header JSON) and `backup_runs(slot UNIQUE, name, result, error)`; ports `Repos.System`, `Repos.BackupRuns`, `AuditRepository.AppendVerbatim` (refuses entries not chaining from the head), `InvoiceRepository.ResetExported`, `EmailRepository.CountSending`; schedule and outbox claim queries read `system_state` `LOCK IN SHARE MODE` and claim nothing while held; new ports (no impl yet): `Snapshotter`, `RestoreStager`/`StagedRestore`, `BackupSealer`, `BackupCodec`, `BackupFiles`; fakes | done | remote-test-int: while held claims return nothing; `AppendVerbatim` refuses a broken link; up/down/up | migrations, queries, mariadb, ports, fakes |
| 14.10 | Use cases (fakes): `backup.go` `CreateBackup` (stored, server key), `ExportBackup(ctx, w, passphrase)` (fresh snapshot streamed, not stored, audited `backup.exported` before streaming), `ListBackups`, `RunDueBackups` (idempotent per slot via `backup_runs`; actor "Backup scheduler"), `PruneBackups` (retention; also legacy `cactusinv-*.sql.gz`; never the newest; one audit entry per pass). `restore.go`: `StageRestore(name, passphrase?)` → `RestorePreview` (header, schema N→M, counts, audit plan, invoices since the backup, whether you can sign in afterwards, warnings), `ApplyRestore(id, currentPassword, confirmation)`, `DiscardRestore`, `ResumeAutomation`; `Housekeeping` discards expired stages. Apply in the stager's single exclusive transaction: refuse if any outbox row is sending (`ErrBusy`); replace every included table; invoice sequence = GREATEST(live, backup); `exported_version` reset so the archive re-renders; SMTP password cleared if this `app_key` cannot decrypt it; all sessions and idempotency keys deleted; audit merge per plan; `automation_held=1`; `backup.restored` entry (source, sha256, schema N→M, audit plan, warnings). Refusals/failures audited in their own transaction (`backup.restore_refused/_failed`) | done | table tests: tampered → `ErrBackupInvalid` + audit; too new; wrong password; wrong confirmation; busy; three merge modes; sequence never rewinds; SMTP cleared/kept; sessions revoked; automation held; app ≥ 80 % | `internal/app/{backup.go,restore.go,emails.go,schedules.go,auth.go}`, tests |
| 14.11 | Adapters: `agecrypt` (`BackupSealer` on `filippo.io/age`: X25519 identity for stored backups, scrypt passphrase for downloads/uploads); `backupformat` (tar.gz stream: `header.json` first, `tables/<name>.jsonl`, `digest.json` last; SHA-256 per table while streaming; limits on decompressed size and row size); `backupdir` (`BackupFiles` on the volume: generated names only, `.part` → fsync → rename, 0600, unknown files ignored, uploads streamed in as ciphertext) | done | round trip; wrong passphrase, flipped byte, truncated → error; fuzz decoder and `Open` (no panic, bounded memory); zip-bomb limit; traversal names rejected | `internal/adapters/{agecrypt,backupformat,backupdir}` |
| 14.12 | MariaDB `Snapshotter`: `START TRANSACTION WITH CONSISTENT SNAPSHOT` over every table in `information_schema` minus an explicit exclude list (`sessions`, `idempotency_keys`, `goose_db_version`, `audit_head`, `system_state`, `backup_runs`); generated columns skipped; typed canonical JSON per row | done | remote-test-int: a table neither exported nor excluded fails the test; values round-trip exactly (DATETIME(6), DATE, BIGINT, BLOB, NULL, utf8mb4) — `make remote-test-int` on the Docker test host after the review fixes: `ok internal/adapters/mariadb 227.713s` | `internal/adapters/mariadb/snapshot.go` |
| 14.13 | MariaDB `RestoreStager`: staging DB `CACTUSINV_RESTORE_DB_NAME` (default `<db>_restore`): drop its tables, goose up to the backup's version, load rows, goose up to head (real migrations, Go ones too), expose its audit chain, counts, SMTP ciphertext. `Apply(ctx, fn)`: `SET autocommit=0`; `LOCK TABLES <live> WRITE, <staging> READ`; delete live child→parent (not `audit_log`); `INSERT … SELECT` parent→child with explicit column lists (no generated columns); run fn (audit merge, events) on the same connection; `COMMIT` then `UNLOCK`; error → `ROLLBACK`. Discard drops staging tables | done | remote-test-int: failure injected halfway leaves live data byte-identical; concurrent writer waits and survives consistent; `audit_log` triggers still reject UPDATE/DELETE afterwards — `make remote-test-int` on the Docker test host after the review fixes: `ok internal/adapters/mariadb 227.713s` | `internal/adapters/mariadb/restore.go` |
| 14.14 | End-to-end integration (testcontainers MariaDB 11 with the staging DB granted by an init script): seed reference data → backup → wipe (migrate down/up) → restore → every included table equal, audit chain verifies, `backup.restored` present; tampered file and a validly re-encrypted archive with one changed row (digest mismatch) refused, live data unchanged; schema N-1 backup restores into head; newer-schema refused; outbox row sending → busy; after restore the dispatcher sends nothing until resumed; three audit merge modes | done | remote-test-int green — `make remote-test-int` on the Docker test host after the review fixes: `ok internal/adapters/mariadb 227.713s` | `internal/adapters/mariadb/backup_integration_test.go` |
| 14.15 | Contract: `GET /backups` (schedule, retention, key configured, last result, stored files with size, sha256, schema), `POST /backups` (Idempotency-Key), `POST /backups/export {passphrase}` → `application/octet-stream` + `Content-Disposition`, `POST /backups/uploads` (octet-stream, ≤ limit) → `{name}`, `POST /restores {backup, passphrase?}` → `RestorePreview`, `GET /restores/current`, `POST /restores/{id}/apply {currentPassword, confirmation}`, `DELETE /restores/{id}`, `POST /automation/resume`; export/upload documented exempt from Idempotency-Key; problems `backup-invalid`, `backup-too-new`, `busy`, `payload-too-large`; `make gen` (confirm oapi-codegen strict mode gives `io.Reader` for an octet-stream body) | done | `make contracts` in sync — done: `make contracts` in sync; strict mode gives `io.Reader` for the octet-stream upload body (confirmed in `api.gen.go`); `nextRun`, `lastRun`, `finishedAt` are pointers (`x-go-type-skip-optional-pointer: false`) | `api/openapi/cactusinv.yaml`, generated |
| 14.16 | Handlers: per-route body limit (`CACTUSINV_BACKUP_MAX_UPLOAD_MB`) and deadlines via `http.ResponseController`; export/upload skip `withIdempotency`; apply clears the session cookie; nothing logs passphrases, PII or row data | done | httptest: oversize → 413; export streams > 1 MiB; apply clears cookie; must-change session can't reach backups (if Part A exists) — done: httptest: oversize upload 413, export > 1 MiB streamed and never replayed with an Idempotency-Key, apply clears the cookie, `ResponseController` deadlines reach the connection through the wrappers. The must-change-session check waits for Part A (14.5) | `internal/adapters/http/*` |
| 14.17 | Wiring/CLI: `CACTUSINV_BACKUP_DIR` (`/data/backups`), `_BACKUP_TIMES`, `_BACKUP_RETENTION_DAYS`, `_BACKUP_MAX_UPLOAD_MB` (256), `_RESTORE_DB_NAME`; optional secret `backup_key` (missing → stored/scheduled backups off with a warning; export still works); worker loop (1 min). CLI: `cactusinv backup keygen`, `backup create [--out F --passphrase-file P]`, `backup list`, `backup restore NAME\|FILE [--passphrase-file P] --yes` (actor "Operator (CLI)"; it audits the file with its SHA-256 before staging, as uploads do — ADR-009 amendment), `backup note-legacy-restore FILE` | done | config_test; worker test (slot once with two runners); binary starts against compose — done: config_test (defaults, TZ, limits), worker test (one slot across two replicas), CLI unit tests; the binary ran against compose on the throwaway stack | `cmd/cactusinv/*`, `internal/adapters/worker/worker.go` |
| 14.18 | Deployment: compose app mounts `${BACKUP_DIR}:/data/backups`; volume-init chowns backups; secret `backup_key`; mariadb initdb script creates `<db>_restore` and grants the app user; backup sidecar removed. `deploy.sh`: generates `backup_key` via `docker run --rm <image> backup keygen` (older installs too); `--install` runs the staging GRANT idempotently via the root secret; `--backup-now` → `compose exec app /cactusinv backup create`; `--restore FILE`: `.age` → in-app restore; `.sql.gz` → legacy path with a warning then `backup note-legacy-restore`. k8s: app mounts backups PVC, CronJob removed, secret patched, grant via `kubectl exec`, memory limit checked. `.env`: `BACKUP_MAX_UPLOAD_MB` | done | Docker install on a throwaway stack (not the user's running local stack): scheduled backup encrypted; `--backup-now` and `--restore` work; `age -d -i secrets/backup_key` opens a stored backup; k8s manifests pass `kubectl --dry-run` — done 2026-10-15 on a throwaway stack on the Docker test host (project `cactusinv-m14test`, ports 18080/18025, removed afterwards): v1.4.0 installed with data and a sidecar `.sql.gz`, then upgraded: `backup_key` generated, staging GRANT applied, sidecar removed, first scheduled backup taken (0600, uid 65532); `--backup-now`, `--restore NAME`, `--restore PATH` (server key and passphrase), legacy `.sql.gz` restore (dump at schema 12 migrated to 14, `backup.legacy_restored` recorded); `age -d -i secrets/backup_key` (age 1.2.1) opened a stored backup; re-install idempotent (same key); manifests: `kubectl apply --dry-run=client` and kubeconform 11/11 valid | `docker-compose.yml`, `deploy/mariadb/initdb/10-restore-db.sh`, `deploy.sh`, `deploy/k8s/cactusinv.yaml`, `deploy/backup/backup.sh`, `.env` |
| 14.19 | Web Settings → Backup & restore: schedule/last result (read-only), stored list, "Back up now", "Download a backup" (passphrase + confirmation, saved as blob), "Restore from file" (upload, then passphrase), preview with warnings, confirmation dialog (current password + type RESTORE), signed-out notice; app-wide "Automatic sending is paused after a restore" banner with Resume (`me.automationPaused`) | done | Vitest; `pnpm -C web check` — done: Vitest (schedule and list, back up now, no-key state, download with confirmed passphrase, restore of a stored backup through preview and confirmation to the signed-out notice, upload → passphrase → preview → discard, size limit, paused banner and resume); `pnpm -C web check` | `web/src/features/settings/backup/*`, `Layout.tsx`, `App.tsx`, `settingsNav.ts` |
| 14.20 | Docs: README (backup, restore, keeping `backup_key` off the host, `age -d`), ARCHITECTURE; full check; throwaway stack: back up, change data, restore, banner, resume, audit verifies | done | make check green — done: README (Backup and restore), ARCHITECTURE §6/§10/§12, VERSION 1.5.0; throwaway stack: back up, change data, restore, banner flag, resume, audit verifies (see 14.18) — `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.8 %, app 85.3 %, "No vulnerabilities found.", "contracts in sync", web 8 files / 32 tests, build OK); `make remote-test-int`: `ok internal/adapters/mariadb 212.334s`. Round-2 review fixes re-checked on a second throwaway stack (`cactusinv-m14r2`, removed): staging grant exactly the list and an old `ALL` narrowed by re-install; safety backup taken, named in `backup.restored`, and restoring it undid the restore; deploy.sh `.age` restore shows the preview, refuses without `--yes` (stage discarded) and applies by id; legacy `.sql.gz`: a broken file and a bad path refused before any change, a valid dump read through `cat-legacy`, safety backup taken and named, loaded as the app user, migrated, recorded; an export whose snapshot connection was killed mid-stream ended with curl exit 18 (transfer closed), not a clean 200 | `README.md`, `docs/ARCHITECTURE.md` |

Open questions answered with the planner's defaults (lead, 2026-10-15): every user is an
administrator; users are disabled, not deleted; forced password change, the default admin
included; re-authentication for add/reset/disable/restore; no PDFs in backups; age with a
≥ 16-character passphrase and a server `backup_key`; automation held after a restore;
invoice numbers never rewind; the plaintext sidecar retired, `.sql.gz` restore kept one
release; schedule and retention from `.env`, read-only in the UI; an undecryptable SMTP
password is cleared; uploads ≤ 256 MiB, confirmation word `RESTORE`, a staged restore
expires after 30 minutes.

Deferred to Part A: the must-change-password session gate (14.5) does not yet cover the
backup routes (14.16 names it); `Me.mustChangePassword` comes with 14.4.

Risks: a restore rolls the business back (the preview counts invoices since the backup;
automation is held); `LOCK TABLES` + `autocommit=0` rollback and cross-database
`INSERT … SELECT` are proven by 14.13; existing installs need the staging-database GRANT
(restore is disabled with a reason until then); memory (age scrypt ~256 MiB transient,
one backup operation at a time); losing `backup_key` or a passphrase makes a backup
unrecoverable; re-sealed audit entries get new hashes (markers record the original head);
legacy plaintext dumps stay until retention prunes them.

### M15 — Warn when the provider will not send as the Send-as address (Albie, 2026-10-16) — v1.6.1

> Send as = accounts@primesoft.co.za, host smtp.gmail.com, user name albievan@gmail.com:
> the test e-mail arrives From albievan@gmail.com, mailed-by gmail.com.

cactusInv already puts the Send-as address in the `From` header and in `MAIL FROM`; Gmail
rewrites `From` to the signed-in account unless the address is a verified "Send mail as"
alias, and Microsoft 365 refuses (`SendAsDenied`) unless the mailbox has Send As
permission. The app cannot see the provider's alias list, so it warns instead.

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 15.1 | Domain `EmailSenderWarnings(SMTPSettings) []string`: Gmail (`smtp.gmail.com`, `smtp.googlemail.com`; not `smtp-relay.gmail.com`) or Microsoft (`smtp.office365.com`, `smtp-mail.outlook.com`) with a Send-as address (net/mail; empty = user name) other than the user name (case-insensitive) → one warning; never nil. Adapter test: the composed message's `From` header and `MAIL FROM` are the Send-as address | done | table tests; smtp adapter test | `internal/domain/smtp.go`, `internal/domain/smtp_test.go`, `internal/adapters/smtp/mailer_test.go` |
| 15.2 | Contract: `EmailSettings.warnings` (readOnly, required, may be empty); `POST /settings/email/test` gains a `200` `TestEmailResult{warnings}` (the `204` stays documented); `make gen` | done | `make contracts` in sync | `api/openapi/cactusinv.yaml`, `internal/adapters/http/api.gen.go`, `web/src/lib/api/schema.d.ts` |
| 15.3 | App `SendTestEmail` returns the warnings of the in-app server it used (none when the deployment's server was used); handlers fill `warnings` from `domain.EmailSenderWarnings` | done | app table test; httptest | `internal/app/smtp.go`, `internal/app/app_test.go`, `internal/adapters/http/handlers_more.go`, `internal/adapters/http/server_test.go` |
| 15.4 | Web: amber note with the server's warnings under Send as; "Test e-mail sent — but: …"; the Gmail and Microsoft guide steps say the same | done | Vitest; `pnpm -C web check` | `web/src/features/settings/Settings.tsx`, `web/src/features/settings/EmailSetupGuide.tsx`, tests |
| 15.5 | Send-as hint "The provider decides the sender it shows; see the setup guide."; VERSION 1.6.1; README e-mail section | done | `make fmt-check lint vet cover vuln contracts web-check` green — done 2026-10-16 on macOS: domain 97.9 %, app 85.6 %, contracts in sync, 41 web tests; integration tests not run (no MariaDB adapter change); not committed, not deployed | `web/src/features/settings/Settings.tsx`, `VERSION`, `README.md` |

### M17 — Editing a recurring-invoice schedule shows its products, prices and run times (Albie, 2026-10-16) — v1.6.2

> Editing a recurring-invoice schedule, every line's product box is empty (quantities shown,
> unit price/amount "—", "Choose a product for every line"); choosing the product again does
> not show it after save and reopen. "Next runs" shows 10:00 for a 09:00 UTC schedule.

Cause (verified read-only in the live DB): `schedule_lines` store `product_id` with an empty
`description` (lines are product-only since M9; a run takes the text from the product), and
the editor used `description` as the product's name; `unitPrice` is returned only for a
fixed-price line, so lines on the product's price had nothing to compute an amount from. The
editor also dropped a fixed price on save. "Next runs" were formatted in the browser's zone,
and a preview without a zone was computed in UTC while saving used the business zone.
(M16 is not used.)

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 17.1 | Contract: `ScheduleLine` gains read-only `productName`, `productStatus` (`active`/`inactive`/`missing`) and `productUnitPrice` (the product's current price in the customer's currency; absent when it has none), read from the product look-up at read time, never from the stored description; `description` unchanged; `RunPreview.timeZone` (required: the zone the runs were computed in — the business zone when the recurrence names none); `make gen` | done | `make contracts` in sync | `api/openapi/cactusinv.yaml`, `internal/adapters/http/api.gen.go`, `web/src/lib/api/schema.d.ts` |
| 17.2 | Domain `ScheduleLine.ProductView(products, currency) ScheduleLineProduct`; app `ListSchedules` returns `ScheduleView` (schedule + one product view per line), `ScheduleDetail.LineProducts`, both from `Products.GetMany` and the customer's currency (no adapter or query change); `PreviewRecurrence(ctx, …)` fills the business zone as `checkSchedule` does and returns it; handlers fill the new fields | done | domain and app table tests (active, inactive, missing, no price in the currency, fixed price); httptest: get/list return `productName`/`productUnitPrice`, preview returns `timeZone` | `internal/domain/schedule.go`, `internal/app/schedules.go`, `internal/adapters/http/{convert,handlers}.go`, tests |
| 17.3 | Web: the editor loads `productName`, `productStatus` and the price (`unitPrice` if fixed, else `productUnitPrice`); a fixed `unitPrice` is kept on save unless another product is chosen; "Product no longer available — choose another." for inactive/missing; "No <currency> price — choose another product." when it has none; next runs, upcoming runs, the list's and the dashboard's next run formatted in the schedule's zone with its name ("Fri 16 Oct 2026, 09:00 UTC") | done | Vitest: an existing schedule shows names and amounts; re-pick + save payload; reopen shows the names; missing/inactive/no-price messages; next-runs label | `web/src/features/schedules/Schedules.tsx`, `web/src/components/LineEditor.tsx`, `web/src/lib/format.ts`, `web/src/features/dashboard/Dashboard.tsx`, tests |
| 17.5 | Schedules list (Albie, 2026-10-16: "change the tiles to a table with compressed buttons to edit, delete, pause/unpause or run schedule manually; add the last time the schedule ran, when is the next run and what type of schedule it is"): table ≥ md (Name, Customer, Type, Repeats, Last run, Next run in the schedule's zone, Status, Actions); cards < md with the same fields and an action row (44 px targets); icon buttons with aria-labels and tooltips: Edit, Pause/Resume, Run now (confirm), Delete (confirm); busy-disabled, success/error notice, list refreshed. Contract: read-only `Schedule.lastRunResult` (`ok`/`failed`, from `domain.Schedule.LastRunResult`: failed while consecutive failures are recorded, ok once it has run) | done | domain table test; httptest; Vitest: columns and labels, each action's request, cancelled confirm sends nothing, pause↔resume, phone cards | `api/openapi/cactusinv.yaml`, generated, `internal/domain/schedule.go`, `internal/adapters/http/convert.go`, `web/src/features/schedules/Schedules.tsx`, `web/src/components/icons.tsx`, tests |
| 17.6 | Default e-mail texts per schedule type (Albie, 2026-10-16: "when I add a new schedule add a professional default e-mail subject and e-mail body for each of the schedule types; for invoices populate the default disclaimer"): domain `ScheduleEmailDefaults()` (recurring invoice, unsent, outstanding, statement; signed with `{{business_name}}`; only placeholders the server fills for that type) and a new `{{date}}` placeholder (the invoice's issue date or the statement's as-at date); contract `GET /schedules/defaults` → `ScheduleEmailDefaults`; a new schedule's form pre-fills subject and body for the chosen type and swaps them when the type changes, never overwriting text the user edited; invoice schedules show the disclaimer printed on every invoice for the chosen customer (read-only, its source from `Customer.disclaimerSource`, the text from the customer or Settings → Business) with a link to edit it — never copied into the notes | done | domain tests (every placeholder supported for its type; `{{date}}`); httptest; Vitest: pre-fill per type, type switch swaps untouched texts, edited text kept, editing an existing schedule changes nothing, disclaimer and source shown | `internal/domain/{email,schedule_email}.go`, tests, `api/openapi/cactusinv.yaml`, generated, `internal/app/schedules.go`, `internal/adapters/http/handlers.go`, `web/src/features/schedules/Schedules.tsx`, tests |
| 17.7 | Monthly day choice and weekend rule (Albie, 2026-10-16: "select last day, first day or the day of the month; if the day of the month falls on a weekend the auto e-mail must be sent on the Friday before the weekend"): `Recurrence.WeekendRule` (`previous-friday` \| `none`) for monthly, quarterly and yearly recurrences (weekly/once: always `none`); a nominal day on Saturday runs 1 day, on Sunday 2 days earlier (the first of a month can move into the previous month and year); `NextOccurrence` stays the nominal instant (period, `{month}`/`{period}`, run-record key) and `NextRunAt`/upcoming/preview are the moved run times; a raised invoice's issue date is its scheduled run date but never after today (17.9); `Activate` picks the first occurrence whose nominal instant is after now and runs it at the later of its moved run time and now (17.9); new schedules default to `previous-friday` (create and preview with the field omitted), an update without it keeps the stored rule; migration `00016_schedule_weekend_rule` (`weekend_rule`, existing rows `none`); the day choice already exists (`dayOfMonth` 0 = last, 1 = first, N clamped to the month's end) — the form offers First day / Last day / Day N and the weekend choice | done | domain table tests (31st in February, leap year, first day on a weekend across a month and a year, last day on a weekend, DST); property test (never later than nominal, at most 2 days earlier, a weekday); app: period from the nominal date, issue date the run date, no double run and no skip on catch-up; `make remote-test-int` (migration up/down/up, round trip); httptest; Vitest — done: the integration tests' pinned `headSchema` moved 15 → 16 (5 tests asserted it); `make remote-test-int` on the Docker test host: `ok internal/adapters/mariadb 231.801s` | `migrations/00016_schedule_weekend_rule.sql`, `queries/schedules.sql`, `sqlcgen/`, `internal/adapters/mariadb/schedules.go`, `internal/domain/{recurrence,schedule}.go`, `internal/app/schedules.go`, `api/openapi/cactusinv.yaml`, generated, `internal/adapters/http/convert.go`, `web/src/features/schedules/Schedules.tsx`, tests |
| 17.8 | Sidebar header: the version sits under the "cactusInv" name beside the mark, no overlap (Albie reported the mark overlapping "v1.6.1 · build 15"; change made by the lead, `data-testid="version"` kept) | done | web tests green; `pnpm -C web check` | `web/src/components/Layout.tsx` |
| 17.9 | Review fixes: `Activate` by nominal instant with `NextRunAt` = max(run, now) (BLOCKER); issue date = min(run date, today); recurring-invoice defaults from the business's own texts; every default rendered through the real invoice and statement e-mail paths; the form leaves the weekend rule to the server (`RunPreview.weekendRule`); `Customers.GetMany` batch; the runner refuses inactive products | done | domain and app tests for the four skip scenarios (rule switched on, created, edited after a failed Friday run, resumed — all on a Saturday) and "ran early, edited: not again"; Run now dated today; inactive product fails the run with the reason; default texts rendered with no `{{` left; `ScheduleEmailsFor` table; Vitest; `make remote-test-int` (`Customers.GetMany`) — done 2026-10-16: local gate exit 0 (domain 97.8 %, app 85.9 %, no vulnerabilities, contracts in sync, web 62 tests, build OK); `make remote-test-int` on the Docker test host: `ok internal/adapters/mariadb 234.484s` | `internal/domain/{schedule,recurrence,schedule_email}.go`, `internal/app/schedules.go`, `internal/ports/{ports.go,fakes/store.go}`, `internal/adapters/mariadb/{catalogue.go,queries/customers.sql,sqlcgen/}`, `api/openapi/cactusinv.yaml`, generated, `web/src/features/schedules/Schedules.tsx`, tests |
| 17.10 | Re-review fixes: `ActivateFrom` keeps an unrun overdue occurrence on edit and on resume after an auto-pause (BLOCKER); `UpcomingRuns` starts with `NextRunAt`; run records keyed by billing period (`period_key`, migration `00017`) | done | domain: `ActivateFrom` table (failed this morning, day moved earlier/later, overdue by months, future pending not pulled forward), `PeriodKey`/`PeriodStart`, lifecycle (auto-pause resume keeps the period, user pause skips); app `TestEditOrResumeKeepsUnrunPeriod` (the review's repro → 1 October invoice; auto-pause → resume → once; user pause → skipped; downtime + edit → September and October; day moved within a billed month → no second invoice, November raised); integration `TestScheduleRunPeriodKey` (backfill incl. a month billed twice, then the day-move case on MariaDB); `headSchema` 17 — done 2026-10-16: local gate exit 0 (domain 97.7 %, app 85.9 %, no vulnerabilities, contracts in sync, web 62 tests, build OK); `make remote-test-int` on the Docker test host: `ok internal/adapters/mariadb 239.430s` | `internal/domain/{schedule,recurrence}.go`, `internal/app/schedules.go`, `internal/ports/{ports.go,fakes/store.go}`, `migrations/00017_schedule_run_period_key.sql`, `internal/adapters/mariadb/{schedules.go,queries/schedules.sql,sqlcgen/}`, tests |
| 17.4 | VERSION 1.6.2; README; gate `make fmt-check lint vet cover vuln contracts web-check` | done | all green — done 2026-10-16 on macOS: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.8 %, app 85.7 %, "No vulnerabilities found.", contracts in sync, web 14 files / 62 tests, build OK); the adapter changed in 17.7, so `make remote-test-int` ran too (green, see 17.7); not committed, not deployed | `VERSION`, `README.md`, `PLAN.md` |

### M18 — Access groups (RBAC) on the Users page (Albie, 2026-10-16) — v1.7.0

> "Add functionality for RBAC on the user page, e.g. I want to be able to create groups and
> assign access to the group or remove access from a group." Also: the active sidebar item's
> outline was clipped at the top of the menu (screenshot).

Design: ADR-012. Users get access only through groups; a group holds permissions from a fixed
catalogue; a user's access is the union of their groups' permissions (with implied view
permissions). The built-in **Administrators** group has every permission plus the
administration areas (users, groups, backups, restore), cannot be renamed or deleted, and
always keeps at least one active member. On upgrade only the default administrator is in
Administrators; everyone else starts with no access (Albie, 2026-10-16). Also asked during
M18: the GitHub repository link on the sign-in page (18.9).

| ID | Step | Status | Acceptance | Files |
|---|---|---|---|---|
| 18.1 | Domain: `Permission` catalogue (areas, labels, implications), `Group` (validate name/description/permissions), `Access` (union + implications; administrator = everything), `AdministratorsGroupID`; `ErrNoAccess` | done | table tests: implications, unknown/duplicate permission, builtin rules, admin has all | `internal/domain/access.go`, tests |
| 18.2 | Migration `00018_access_groups`: `access_groups`, `access_group_permissions`, `access_group_members`; seed Administrators; every existing user a member | done | migrations up/down/up; integration round trip | `migrations/00018_access_groups.sql` |
| 18.3 | Ports `GroupRepository` (+fake), MariaDB adapter, sqlc queries; `LockAdministrators` (active members, FOR UPDATE) | done | integration test: CRUD, members, permissions, lock | `internal/ports`, `internal/adapters/mariadb` |
| 18.4 | App: every API operation mapped to the permission it needs (unknown operation = refused); `SessionAllows` checks it; `Authenticate` loads the user's access; groups use cases (list, create, update, delete, set a user's groups) re-authenticated, audited, refusing to remove yourself from Administrators and serialised on the Administrators lock; new and CLI/default users join groups | done | app tests: every operation mapped (checked against the contract in the HTTP tests), access denied, lockout rules, audit entries | `internal/app/{access,groups,auth,users}.go`, tests |
| 18.5 | Contract: `Me.permissions`/`administrator`, `User.groupIds`, `NewUserRequest.groupIds`, `/groups` (list incl. the permission catalogue, create), `/groups/{id}` (update), `/groups/{id}/delete`, `/users/{id}/groups`; 403 `no-access`; handlers | done | httptest: a user without a permission gets 403, admin-only routes, group CRUD | `api/openapi/cactusinv.yaml`, generated, `internal/adapters/http` |
| 18.6 | Web: `useCan`; menus, routes and action buttons follow the signed-in user's access; Users page gets Users / Groups tabs: groups table, group editor (permissions by area, implied ones shown as included, members), a user's groups, groups when adding a user | done | Vitest: nav filtered, no-access page, group create/update payloads, user groups payload | `web/src` |
| 18.7 | Sidebar: the active item's outline is no longer clipped and the menu lines up with the search box | done | rendered at 1280 px, folded and with Settings open | `web/src/components/Layout.tsx` |
| 18.8 | VERSION 1.7.0; README, ARCHITECTURE, ADR-011 note, ADR-012; `make check` | done | see the evidence line below | docs |
| 18.9 | Sign-in page: "Free and open source: github.com/albievan/cactusinv" under the feature list, and "Source on GitHub" beside the version | done | rendered | `web/src/components/auth.tsx` |

Evidence (2026-10-16, isolated worktree at 27bf815 + M18): `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.7 %, app 85.5 %, "No vulnerabilities found.", contracts in sync, web 68 tests, build OK); `make test-int` on local Docker: `ok internal/adapters/mariadb 34.9s` (incl. `TestGroupRepository`, `TestAccessGroupsMigrationSeedsOneAdministrator`, `headSchema` 18); screens checked at 1280 px and 390 px with mocked data (users, groups, group editor, sign-in page).


### M19 — Bug hunt across the whole codebase (Albie, 2026-10-16) — v1.7.1

> "Read this whole codebase and find real bugs, broken edge cases, and anything that falls
> over in front of a user. List everything by severity and fix anything until no more bugs
> exist." Six reviewers (domain, use cases, HTTP/security, MariaDB, web, workers/PDF/deploy)
> reported verified findings with repros; each was checked against the code and fixed with a
> test. Severity as found.

| ID | Sev. | Defect → fix | Status | Test |
|---|---|---|---|---|
| 19.1 | High | Invoice totals overflowed int64 (10 lines at the limits → a negative or garbage total, 201) → `checkTotalsFit`: subtotal and total ≤ `MaxInvoiceTotal` (1e15 minor units), summed in big.Int, 422 | done | `domain/bugfix_test.go` |
| 19.2 | High | A user with only `invoices.send` could issue drafts through "Send" → sending a draft needs `invoices.edit` (`Actor.Can`); the "Send later" button follows | done | `app/bugfix_test.go` |
| 19.3 | High | `dashboard.view` exposed the audit log, outbox, invoices and schedules → the dashboard drops what the user may not open | done | app |
| 19.4 | High | `statements.view` listed the whole outbox → without `emails.view` only one customer's statement e-mails | done | app |
| 19.5 | High | Shutdown during an SMTP send left the e-mail "sending" → sent again after restart → the outcome is recorded on a context that survives shutdown (`outcomeContext`); workers stop first; compose `stop_grace_period: 30s` | done | app suite, review |
| 19.6 | High | An error on QUIT after the server accepted the message made the outbox retry (duplicate) → QUIT errors ignored | done | `smtp/bugfix_test.go` |
| 19.7 | High | PDF rows taller than the space left, and long notes/disclaimers, were drawn below the page → rows split across pages, notes continue page after page | done | `pdf/bugfix_test.go` |
| 19.8 | High (web) | Product picker unusable on phones (name column 0 px) → stacked layout below `sm` | done | rendered |
| 19.9 | High (web) | Decimal comma ("192,94" from comma-region iPhones) refused everywhere → read as a point unless ambiguous ("1,500"), in the web app and `domain.parseFixed` | done | web + domain |
| 19.10 | Medium | Cancel/void racing the dispatcher could be lost and the e-mail still sent → `Emails.Lock` (FOR UPDATE) in every transition; outcomes only recorded while "sending" | done | `mariadb/bugfix_integration_test.go` |
| 19.11 | Medium | "Unsent" e-mail schedules re-sent invoices with an e-mail already queued → skipped | done | app |
| 19.12 | Medium | `schedules.edit` alone could make schedules that invoice or e-mail → invoice schedules need `invoices.edit` (+ `invoices.send` to auto-send), e-mail ones `invoices.send` / `statements.send`; `schedules.view` now implies `customers.view` and `products.view` | done | app |
| 19.13 | Medium | Send `Idempotency-Key` answered with another invoice's e-mail when reused → refused (409) unless it was this invoice's / customer's | done | app |
| 19.14 | Medium | Password bodies on PUT routes (groups, user groups, e-mail server) were fingerprinted for idempotency → never stored | done | http |
| 19.15 | Medium | Rate-limit buckets could be flushed by spraying addresses; IPv6 not grouped → evict only idle buckets; IPv6 keyed by /64 | done | review |
| 19.16 | Medium | Parser accepted "-", "+", "." as 0 → refused | done | domain |
| 19.17 | Medium | Customers could have more CCs than any e-mail allows → max 10, duplicates dropped | done | domain |
| 19.18 | Medium | PDF text outside cp1252 printed as dots → DejaVu Sans Condensed embedded (UTF-8; licence in `internal/adapters/pdf/fonts/LICENSE`) | done | pdf |
| 19.19 | Medium | Long non-ASCII subjects / many CCs made header lines > 998 bytes → folded | done | smtp |
| 19.20 | Medium (web) | Statement "Owed to you" summed floats, always 2 decimals (KWD/JPY wrong); dashboard zeros "0.00" → exact minor-unit sums, the currency's decimals | done | web |
| 19.21 | Medium (web) | One tap deleted drafts, products, customers, bank accounts, schedules; schedule Run now ran at once → confirm sheets (`ConfirmButton`) | done | web |
| 19.22 | Medium (web) | Statement e-mail could be queued twice by a double tap → busy guard | done | review |
| 19.23 | Medium (web) | Session timeout replaced the page, losing a half-filled form → sign-in over the page | done | web |
| 19.24 | Medium | `deploy.sh --restore FILE --ssh-access` could not use a local file or a passphrase → file copied privately; passphrase over stdin | done | `bash -n`, shellcheck |
| 19.25 | Medium | k3s/k8s re-install from a dirty tree kept the old pods → unique tag per dirty build | done | review |
| 19.26 | Medium | k8s liveness could kill a pod mid-migration; replicas could migrate at once → `startupProbe` (15 min) and a `GET_LOCK` migration lock | done | integration |
| 19.27 | Low–Med | Dashboard "paid" months in UTC; top debtors ranked across currencies → business-zone months; top N per currency | done | integration |
| 19.28 | Low | Schedule runs locked audit head before the invoice sequence (deadlock with manual issue); run counts inflated on retry → sequence locked first; counted after commit | done | app suite |
| 19.29 | Low | Failing archive exports blocked all others → 15-minute back-off per invoice | done | app |
| 19.30 | Low | Values longer than their column were 500s → 422 naming the column (MariaDB 1406/1366) | done | integration |
| 19.31 | Low | Number format could expand past the 64-character column; schedule descriptions past 2000 after placeholders → checked at their longest | done | domain |
| 19.32 | Low | Daylight-saving gap at midnight moved a run to the next date; 30-minute gaps fired late → pre-change offset, never another date | done | domain |
| 19.33 | Low | Idempotency keys > 163 chars or non-ASCII were 500s; a panic left a key "in progress" 24 h → max 128 printable ASCII; released on panic | done | http |
| 19.34 | Low | Oversized JSON was 400 (413 with a key); SPA listed /assets/ and answered missing chunks with index.html; unknown customer 404 → 413; 404; 422 `customerId` | done | http |
| 19.35 | Low | Failed test e-mail audited as sent; SMTP errors leaked internal errors; "scheduled for" in UTC → audited as failed; only mail-server errors shown; business zone | done | app |
| 19.36 | Low | Restore preview: empty SMTP password read as "cleared"; restorer not warned when not an administrator in the backup → nil when absent; warning | done | app |
| 19.37 | Low (web) | Schedule start date in UTC at module load; inactive customer hid a schedule's lines; repricing on currency change; "Send test e-mail" for viewers / tested unsaved settings; Business page spun forever on error; settings-only users saw "no access" at home; product search races and errors; same backup file twice; links to pages without access | done | web suite |

Known limits left as they are (committed migrations are never edited, CLAUDE.md §5): the Down
steps of 00004 (overdue drafts back to "sent"), 00009 (no-op; re-running Up 9 alone backfills
twice) and 00017 (fails if two runs share a date under different period keys). Weekly
schedules can still bill a week twice when their weekday moves (accepted in M17).

Evidence (2026-10-16, worktree at 5afff07 + M19): `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.8 %, app 85.6 %, "No vulnerabilities found.", contracts in sync, web 73 tests, build OK); `make test-int` on local Docker: `ok internal/adapters/mariadb 34.2s` (incl. the new lock, local-month, per-currency, too-long and migration-lock tests); product picker measured at 360 px (name column 213 px, was 0); sample PDFs rendered with the embedded font.

---


### M20 — Access per action; schedules in the right time zone; k3s deploy (Albie, 2026-10-16) — v1.8.0

> 1. "I've set up a weekly invoice and the first run was supposed to be today at 8am.
>    Investigate and fix the issue."
> 2. "I need the access to be more granular, e.g. I want to select to add a customer but not
>    edit, and create invoice but not edit or issue or delete etc."
> 3. "I've tried to deploy the platform to k3s on 192.168.1.72 and it timed out."

Findings (1), read-only on the live instance: the schedule *did* run — invoice raised at
08:00:54 UTC and e-mailed at 08:01 UTC — but in **UTC**: the business profile had been saved
with the default zone `UTC` (`domain.BusinessProfile.Validate`, prefilled on the form) while
the deployment runs in `TZ=Africa/Johannesburg`, so "08:00" fired at 10:00 local. The
schedule editor had no time-zone field, so a schedule kept the zone it was created with and
could not be corrected from the UI.

| id | Step | Status | Verification | Files |
|---|---|---|---|---|
| 20.1 | Business zone defaults to the deployment zone (`app.Deps.TimeZone` ← `TZ`; unknown → UTC) before the profile is first saved | done | `TestBusinessZoneDefaultsToTheDeploymentZone` (08:00 Johannesburg = 06:00 UTC) | `internal/app/app.go` (`business` is now a method), `cmd/cactusinv/main.go` |
| 20.2 | Time zones picked from a list (`TimeZoneSelect`, device zone first) on Settings → Business and in the schedule editor; the editor shows the first run in device time when the zones differ | done | Vitest `time zones are picked from a list` | `web/src/components/TimeZoneSelect.tsx`, `Settings.tsx`, `Schedules.tsx` |
| 20.3 | One permission per action (ADR-012 table): invoices create/edit/issue/delete/send/pay/overdue/void; customers and products create/edit/delete; schedules create/edit/pause/run/delete; outbox cancel/retry; settings business/email/currencies/banks. Actions imply only views. Migration `00019` maps old grants to all the actions they covered (down folds them back) | done | `TestEachActionHasItsOwnPermission` (each permission allows exactly its operations), `TestActionPermissionsImplyOnlyWhatTheyNeed`, schedules need `invoices.create`+`invoices.issue`, sending a draft needs `invoices.issue`; integration `TestGranularPermissionsMigration` (old grants → new, down → old); Vitest (add-not-edit customer form, per-action invoice buttons) | `internal/domain/access.go`, `internal/app/{access,schedules,invoices}.go`, `migrations/00019_granular_permissions.sql`, web pages, tests |
| 20.4 | Web shows each button only with its own permission; a page the user may open but not change shows no Save/Delete | done | Vitest `granular.test.tsx` | `web/src/App.tsx`, features/* |
| 20.5 | k3s deploy: build without provenance/SBOM, `docker save --platform`, verify the image on the node, `imagePullPolicy: Never` on k3s, root or `sudo -n` for `k3s ctr`/`kubectl` over SSH, and on a rollout timeout print pods, events and logs | done | `bash -n`; manifest render shows `imagePullPolicy: Never`; local Docker Desktop deploy still green | `deploy.sh`, `deploy/k8s/cactusinv.yaml`, `README.md` |
| 20.7 | k3s root cause (from the screenshots and, read-only, `kubectl describe` on 192.168.1.72): MariaDB never started — `StartError … mkdirat …/rootfs/run/secrets/kubernetes.io: read-only file system`, 151 restarts in 12 h. The service-account token mounts at `/var/run/secrets/kubernetes.io` (= `/run/secrets/…`), inside our read-only secrets volume at `/run/secrets`; Docker Compose has no token, so it never showed locally. Fix: `automountServiceAccountToken: false` on every pod (none uses the API). Also: a re-install failed on volume claims created larger than `.env` (100Gi/100Gi/30Gi; a claim cannot shrink), so an existing claim keeps its size; rollout diagnostics include the previous container's log | done | `kubectl apply --dry-run=server` on the node: all resources accepted; `./deploy.sh --install --k3s --ssh-access root@192.168.1.72` exit 0 (2026-10-16, commit `548413e`): image `cactusinv:548413e` imported and found on the node, existing claims kept at 100Gi/100Gi/30Gi, `backup_key` generated; mariadb, cactusinv and mailpit 1/1 Running, 0 restarts; `http://192.168.1.72:30080` 200, `/version` `{"version":"1.8.0","build":"20"}`; no error lines in the app log | `deploy/k8s/cactusinv.yaml`, `deploy.sh`, `README.md` |
| 20.6 | VERSION 1.8.0, docs, gate, integration, deploy | done | 2026-10-16: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.8 %, app 85.6 %, "No vulnerabilities found.", contracts in sync, web 77 tests, build OK); one earlier `make cover` run failed once and did not reproduce in six reruns (recorded as a possible flaky test); `go test -race -count=1 -tags integration ./internal/adapters/mariadb/` ok 34.7 s (`headSchema` 19, `TestGranularPermissionsMigration`); Docker Desktop: backup `cactusinv-20261016T154030Z-s18-manual.age` taken first, then v1.8.0 build 19, schema 19, groups migrated ("Invoice Status" → `invoices.overdue`, `invoices.pay`, `invoices.void`), data intact (4 customers, 10 invoices); k3s: see 20.7. Docs (2026-10-17): ADR-007 amendment (k3s pods), ARCHITECTURE §10, README k3s notes | `VERSION`, `README.md`, `docs/`, `PLAN.md` |

The live schedule and business profile were **not** changed (real data): set Settings →
Business → Time zone, then edit the "Weekly" schedule's time zone (both are now pick-lists).

### M21 — Billing periods from the start date; placeholders documented; flaky test (Albie, 2026-10-17) — v1.9.0

> Q1 "finish the billed-period work": yes. Q2 periods count from the **start date**. Q3 an
> edit after a billed period **continues** in the next unbilled one. Q4 resuming **retries**
> a period that had failed. Q7 find and fix the flaky test. Later: "document all
> placeholders that can be used in emails and email subjects".

| id | Step | Status | Verification | Files |
|---|---|---|---|---|
| 21.1 | Domain: `Recurrence.BillingPeriod` (anchored on the start date: interval × 1/3/12 months from the start month, interval weeks from the Monday of the start week, a day for once); `ActivateFrom(pending, billedUntil, now)` never starts inside billed time; `Resume(now, billedUntil)` retries a failed pending period (`HasFailedPending`, also after a user pause); `ScheduleRun.PeriodStart/PeriodEnd`, `Overlaps`; `PeriodKey` is a label; `PeriodStart`, `PausedByFailures`, the draft's unused `IsOccurrence` removed | done | `TestBillingPeriod` (11 cases incl. before the anchor, 2-weekly), `TestActivateFrom` (floor cases), `TestScheduleRunOverlaps`, lifecycle | `internal/domain/{recurrence,schedule}.go`, tests |
| 21.2 | Ports/adapters: `InsertRun` refuses an overlapping period; `BilledUntil`; migration `00020` adds `period_start`/`period_end`, backfills them under each schedule's recurrence (a period billed twice keeps its first run, the others their day), primary key `(schedule_id, period_start)`, `period_key` a non-unique label; down folds duplicate labels to dates | done | integration `TestScheduleRunBillingPeriod` (backfill = `domain.BillingPeriod` for monthly, 2-monthly, quarterly from February, yearly from April, 2-weekly, once; `BilledUntil`; overlap refused, adjacent accepted, a reused label for an unbilled period accepted; down → labels unique → up); `TestScheduleRunPeriodKey` through 00020; `headSchema` 20 | `migrations/00020_schedule_run_billing_period.sql`, `internal/adapters/mariadb/{schedules.go,queries/schedules.sql,sqlcgen/}`, `internal/ports/{ports.go,fakes/store.go}` |
| 21.3 | App: edit and resume pass `BilledUntil`; runs record their period; `PreviewScheduleEdit` and `POST /schedules/preview?scheduleId=` (the editor previews an edit as saving would set it) | done | `TestQuarterlyPeriodsCountFromTheStartDate`, `TestEditContinuesAfterTheBilledPeriod` (monthly → quarterly, Thursday → Friday, day 15 → 20; preview = saved; nothing billed twice; next raised), `TestResumeRetriesAPeriodThatFailedBeforeThePause`; `TestWeekendRuleNeverSkips` (ran early Friday, edited Saturday → December pending directly); httptest preview with `scheduleId`, 404 unknown | `internal/app/schedules.go`, `api/openapi/cactusinv.yaml`, generated, `internal/adapters/http/handlers.go`, `web/src/features/schedules/Schedules.tsx`, tests |
| 21.4 | Placeholders documented: e-mail `{{…}}` (invoice and statement e-mails), invoice number `{…}`, schedule line descriptions `{month}`/`{year}`/`{period}`, with values and examples; ARCHITECTURE's outbox note corrected (plain substitution, not `text/template`) | done | each placeholder checked against `domain.RenderTemplate`, `TemplateDataFor`, the statement e-mail data, `numberReplacer`, `ValidateNumberFormat`, `BuildDraft` | `docs/PLACEHOLDERS.md`, `README.md`, `docs/ARCHITECTURE.md` |
| 21.5 | Flaky test: `TestAFailingExportDoesNotBlockTheOthers` failed about 1 run in 11 (`first pass = {Exported:1 Failed:0}`). Cause: the fake store's `ListUnexported` applied the limit while ranging over a map (random order) and sorted afterwards by id, unlike the SQL (`ORDER BY updated_at LIMIT ?`); with limit 1 it returned either invoice. Fix: sort by `updated_at` (id as tie-break), then limit. The app and SQL were right. Also: `cover` hid the failing test's output (`>/dev/null`) | done | before: 45/500 targeted runs and 5/20 full runs failed, all this test; after: 0/2000 targeted, 0/600 with `-race`, 0/50 full runs under load (`-cpu 1,4,8`, parallel loops), 0/300 again after merging | `internal/ports/fakes/store.go` |
| 21.6 | ADR-004 amendment, ARCHITECTURE §3.7, README; VERSION 1.9.0; gate; integration; deploy | done | 2026-10-17: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.7 %, app 85.4 %, "No vulnerabilities found.", contracts in sync, web 77 tests, build OK); integration ok 47 s (`headSchema` 20); Docker Desktop (192.168.1.191): backup `cactusinv-20261017T032124Z-s19-manual.age` first, then v1.9.0 build 22, schema 20, 4 customers and 10 invoices, the Weekly run of 16 Oct backfilled as [12 Oct, 19 Oct), 0 error lines, LAN 200; k3s (192.168.1.72): image `cactusinv:9bb6d89` on the node, v1.9.0 build 22, all pods 1/1 Running, 0 error lines | `docs/`, `README.md`, `VERSION` |

### M22 — Settings → Import/Export Settings (Albie, 2026-10-17) — v1.10.0

> "add functionallaty under the settings menu 'Import/Export Settings' to allow me to import
> and export customers, currencies, bank accounts, business, emailserver to a json file"

| id | Step | Status | Verification | Files |
|---|---|---|---|---|
| 22.1 | App: `ExportSettings` (one snapshot transaction; audit `settings.exported`) and `ImportSettings(file, apply)`: one transaction, all or nothing, nothing deleted; currencies by code (minor units never change), bank accounts by id then currency + account number (the first active account of a currency is its default, as on create), customers by code (a file's bank account id is mapped to the account here), business (next sequence only forward, else a warning), e-mail server (stored password kept; imported switched off, with a warning, when it needs a password none is stored for); a check (`apply` false) rolls back; errors name the item (`customers[CD].email`) | done | `TestSettingsMoveToANewInstallation` (check saves and audits nothing; import; an invoice issued afterwards uses the imported number format and override account), `TestSettingsReimportUpdatesAndNeverGoesBack` (same file → all unchanged; updates in place; next number kept, then moved forward; password kept; a missing section untouched), `TestSettingsImportIsAllOrNothing` (8 refusals, nothing partly saved) | `internal/app/settings_transfer.go`, tests |
| 22.2 | API: `GET /settings/export`, `POST /settings/import?apply=` with `SettingsFile` (format `cactusinv-settings`, version 1, reusing the Business/EmailSettings/Currency/BankAccount/Customer schemas) and `SettingsImportResult`; administrators only (`administer`); a password in the file is ignored; 16 MiB body limit on the import route | done | httptest `TestSettingsFileOverHTTP` (export, check, apply with Idempotency-Key, password ignored, wrong format/newer version/bad item → 422 naming the field, a user with every grantable permission but not administration → 403); `TestEveryOperationHasAnAccessRule`; limits table | `api/openapi/cactusinv.yaml`, generated, `internal/adapters/http/handlers_settings_file.go`, `handlers_backup.go`, `internal/app/access.go` |
| 22.3 | Web: Settings → Import/Export Settings (`/settings/import-export`, administrators): export downloads `cactusinv-settings-YYYY-MM-DD.json`; import reads the file, lets you choose its sections, checks it on the server (new / changed / unchanged per section, warnings, errors) before **Import** | done | Vitest `ImportExport.test.tsx` (download content, check then import of the chosen sections only, not-JSON / not-a-settings-file / server error, hidden for non-administrators) | `web/src/features/settings/transfer/ImportExport.tsx`, `settingsNav.ts`, `App.tsx`, `components/icons.tsx` |
| 22.4 | MariaDB: a check really rolls back; the same file matches every version; a new default account clears the old one; a customer follows the file's account id | done | integration `TestSettingsFileImport` | `internal/adapters/mariadb/settings_file_integration_test.go` |
| 22.5 | Second part (Albie's answers, 2026-10-17: "Push and redeploy, Products & schedules too, Passphrase-protect the file"): products (by SKU) and schedules (by customer code + name; lines name products by SKU; a new schedule is created **paused**, a matched one keeps its run state and billed periods via the edit path) in the file; `POST /settings/export {passphrase?}` returns a file holding only `encrypted` (age scrypt, the backup sealer; `age -d` opens the base64-decoded bytes); `POST /settings/import {file, passphrase?, sections?}`; both routes exempt from Idempotency-Key (passphrase); the web page protects exports by default (opt-out for readable JSON) and opens a protected file with its passphrase | done | app `TestSettingsMoveToANewInstallation` (+ products, a schedule with a unit-price override imported paused and not run when due), `TestSettingsReimportUpdatesAndNeverGoesBack` (+ product renamed, schedule day changed stays active), `TestSettingsImportIsAllOrNothing` (+ unknown customer/product, unknown price currency), `TestSettingsFileWithAPassphrase`; httptest protected export/open, wrong/missing passphrase 422, sections; Vitest passphrase export, protected open; integration: a schedule and products round-trip unchanged on MariaDB, real age seal/open | `internal/app/{settings_transfer,schedules}.go`, contract, handlers, web, tests |
| 22.6 | README, ARCHITECTURE §16; VERSION 1.10.0; gate; integration; push; deploy | done | 2026-10-17: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.7 %, app 85.3 %, "No vulnerabilities found.", contracts in sync, web 83 tests, build OK); integration ok 37 s; pushed `f65f29f`; Docker Desktop (192.168.1.191): backup `cactusinv-20261017T094006Z-s20-manual.age` first, then v1.10.0 build 25, schema 20 (no migration), 4 customers, 10 invoices, 5 products, 2 schedules, 0 error lines, LAN 200; k3s (192.168.1.72): v1.10.0 build 25, all pods 1/1 Running, 0 error lines | `README.md`, `docs/ARCHITECTURE.md`, `VERSION` |

### M23 — Settings → System Settings (Albie, 2026-10-17) — v1.11.0

> "under the Settings menu add a new sub-menuitem call System Settings and allow me to set the
> session timeout and allow me to change the folder settings for the backup, export etc." —
> "i want to be able to map the container folders"

| id | Step | Status | Verification | Files |
|---|---|---|---|---|
| 23.1 | Domain: `SystemSettings` (idle timeout 0 or 5 min–30 days, maximum 1 h–365 days, idle ≤ maximum; host folders "" or absolute or `./`, no `..`, `:`, `$`, quotes, backslashes or control characters), `SessionAlive` (expiry, maximum from the start, idle from the last use); `Session.LastSeenAt` | done | `TestSystemSettingsValidate` (12 refusals), `TestSessionAlive` | `internal/domain/system_settings.go`, `user.go` |
| 23.2 | Migration `00021`: `system_settings` (one row, never backed up: added to `backupExcluded`), `sessions.last_seen_at`; repository, `Sessions.Touch` | done | integration `TestSystemSettingsStore` (save, version conflict, last use written, idle sign-out on MariaDB, not in a snapshot, down to 20 and up); `headSchema` 21 | `migrations/00021_system_settings.sql`, `internal/adapters/mariadb/{system_settings.go,outbox.go,queries/}`, ports, fakes |
| 23.3 | App: `Get/UpdateSystemSettings` (audit `settings.system_updated`), `FolderMappings`; sign-in lasts the maximum; every request checks the timeouts (also for sessions started earlier) and records use at most once a minute — always, so switching an idle timeout on does not sign out active users | done | `TestSessionTimeoutsApplyToEverySession` (found that bug: use was only recorded with an idle timeout set), `TestSystemSettingsFolders` (`./` resolved against the project folder, pending until redeployed, none on k3s) | `internal/app/{system_settings,auth,app}.go` |
| 23.4 | API `GET/PUT /settings/system` (administrators); CLI `cactusinv system folders`; deploy: compose passes the deploy target and the absolute host folders mounted now; `deploy.sh --install --dockerdesktop` reads the mapping from the running app (environment still wins), copies the old folder's files into an empty new one, and mounts it; k8s config map names the target | done | httptest `TestSystemSettingsOverHTTP`; `bash -n deploy.sh`; the folder rule on sample lines (absolute, `./` with a space accepted; `:`, `$`, relative, `..` refused) | `api/openapi/cactusinv.yaml`, handlers, `cmd/cactusinv/{main,config}.go`, `deploy.sh`, `docker-compose.yml`, `deploy/k8s/cactusinv.yaml` |
| 23.5 | Web: Settings → System Settings (`/settings/system`, administrators): idle minutes, maximum hours, each container folder with the host folder mapped now and the one to map; on Kubernetes the folders are shown as volume claims | done | Vitest `SystemSettings.test.tsx` | `web/src/features/settings/system/` |
| 23.6 | README, ARCHITECTURE §17, VERSION 1.11.0; gate; integration | done | 2026-10-17: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.8 %, app 85.4 %, "No vulnerabilities found.", contracts in sync, web 85 tests, build OK); integration ok 38.6 s (`headSchema` 21); not yet deployed | `README.md`, `docs/ARCHITECTURE.md`, `VERSION` |

### M24 — Access per page and action in Settings; the new pages grantable (Albie, 2026-10-17) — v1.12.0

> "add the new menu items to the RBAC list and split each access into indevidual access"

| id | Step | Status | Verification | Files |
|---|---|---|---|---|
| 24.1 | Domain catalogue: `settings.view` → `settings.business.view`, `settings.email.view`, `settings.currencies.view`, `settings.banks.view`; `settings.currencies` → `.create`/`.edit`; `settings.banks` → `.create`/`.edit`/`.delete`; new `settings.email.test`, `schedules.resume`, `audit.verify`; the new pages: `settings.export`, `settings.import`, `settings.system.view`/`.edit`; areas named per Settings page in the group editor; `Permission.Label` | done | `TestEachActionHasItsOwnPermission` (each new permission allows exactly its operation), `TestActionPermissionsImplyOnlyWhatTheyNeed` | `internal/domain/access.go`, `internal/app/access.go` |
| 24.2 | Migration `00022`: old grants → all the new ones they covered; down folds back | done | integration `TestSplitSettingsPermissionsMigration` (two groups up and down); `headSchema` 22 | `migrations/00022_split_settings_permissions.sql` |
| 24.3 | An import needs each action's permission for what it adds or changes (unchanged items need none), named in the check | done | `TestSettingsImportNeedsEachActionsPermission` | `internal/app/settings_transfer.go` |
| 24.4 | Web: menu, routes and buttons per permission (`can` takes any of several): Settings pages, + Currency / Save, + Bank / Save / Delete, the bank list, test e-mail, Resume, Verify integrity, Export and Import cards, System Settings Save | done | Vitest `granular.test.tsx` (bank edit without add/delete; currencies without banks), `ImportExport.test.tsx` (export only) | `web/src/components/{auth,Access}.tsx`, `App.tsx`, features |
| 24.5 | ADR-012 amendment, VERSION 1.12.0; gate; integration | done | 2026-10-17: `make fmt-check lint vet cover vuln contracts web-check` exit 0 (domain 97.5 %, app 85.3 %, "No vulnerabilities found.", contracts in sync, web 86 tests, build OK); integration ok 33.4 s (`headSchema` 22); not yet pushed or deployed | `docs/adr/ADR-012-access-groups.md`, `VERSION` |

### M25 — Full bug hunt (Albie, 2026-10-17) — v1.12.1

> "Read this whole codebase and find real bugs, broken edge cases, and anything that falls
> over in front of a user. List everything by severity and fix anything until no more bugs
> exist." Four parallel read-only reviews (settings/sessions/deploy; invoices/money/PDF;
> schedules/outbox/SMTP; HTTP/auth/backups/web), every finding traced, fixed with a
> regression test, then a second review of the fixes themselves.

| id | Severity | Finding | Fix | Verification |
|---|---|---|---|---|
| 25.1 | High | A System Settings folder mapping could be `./data`, `./data/mariadb`, `./secrets`, a home or system folder: the next deploy's volume-init re-owned it recursively (MariaDB down, secrets unreadable) | `ValidateHostDir` refuses system, home, database, secrets and the whole data folder and the same folder twice; `deploy.sh folder_safe` refuses the installation's own MARIADB_DATA_DIR/SECRETS_DIR (and inside or above them), the project folder, overlapping mappings and a folder holding files cactusInv did not write | `TestSystemSettingsValidate` (+7), folder rule on samples |
| 25.2 | Medium (security) | Sign-in timing told which logins exist: the dummy hash was malformed, refused before hashing (30 µs vs 67 ms) | a real hash made by the configured hasher, once | `TestUnknownLoginRunsARealPasswordCheck` |
| 25.3 | Medium (security) | `PUT /groups/x%2Fy` bypassed the password rate limit and Idempotency exemption (decoded path had an extra segment) | every PUT under `/groups/` is a reauth route | `TestReauthPath` (+3) |
| 25.4 | Medium | An e-mail the server accepted was sent again when marking the invoice sent conflicted (payment recorded meanwhile, two messages) | the message is marked sent in its own transaction first; the invoice step retries and never feeds the retry path; MarkSent writes only from issued | `TestAnAcceptedEmailIsNeverSentAgain` (fails before) |
| 25.5 | Medium (security) | A schedule saved without `kind` needed only `invoices.send`, yet raises and issues invoices | `""` is checked as an invoice schedule | existing granular tests |
| 25.6 | Medium | Deactivating a customer's bank account blocked issuing, sending and schedules for that customer | an inactive override falls back to the currency default; the editor shows the inactive account | `TestResolveBankAccount`, `TestDeactivatedCurrencyOrAccountDoesNotLockCustomers` |
| 25.7 | Medium | Deactivating a currency made its customers uneditable | an active currency/account is needed only when newly chosen | same test |
| 25.8 | Medium | Changing a customer's currency dropped its unpaid invoices from statements | refused while issued, sent or overdue invoices exist (also on import) | `TestCurrencyChangeWaitsForOpenInvoices` |
| 25.9 | Medium | Saving Business after an invoice was issued failed ("may only increase"), and a concurrent issue could be numbered again | the form sends the next number only when changed; the sequence is locked before compare (also on import) | web build, app tests |
| 25.10 | Medium | Folder mappings were silently dropped when the app was not running; a cleared mapping never moved the files back | mappings read after the database is up, from the running app or a one-off container; both folders moved when they change; delete-uninstall says mapped folders are kept | `bash -n`; regression review |
| 25.11 | Medium | A protected settings file could not be opened when any section was not allowed | `inspect=true` opens and counts without checking | httptest inspect, Vitest |
| 25.12 | Medium | Importing customers without their bank accounts failed across installations | the file's accounts are matched by currency + number; a missing one gives the default with a warning | `TestImportedCustomersFindTheirBankAccount` |
| 25.13 | Medium (security) | An imported e-mail server reused this server's stored password for another host or account | the password is dropped (with a warning) when host or user differ | `TestImportedEmailServerNeverGetsThisPassword` |
| 25.14 | Medium | E-mail schedules looked at only the customer's 200 newest invoices | all open invoices, paged | app tests |
| 25.15 | Medium-low | A schedule in another zone than the business dated its invoice a day early | dated in the schedule's zone | `TestScheduleInvoiceIsDatedInTheSchedulesZone` |
| 25.16 | Medium-low | Editing a draft re-priced its lines and reset bill-to, ship-to and terms | the editor sends them | Vitest `InvoiceEditor.test.tsx` |
| 25.17 | Medium-low | A product priced in a deactivated currency could not be saved | its own currency stays selectable | web build |
| 25.18 | Medium-low | The invoice editor, invoice, customer and product lists stopped at 200 | customer search in the editor; Show more paging (`usePaged`) | Vitest |
| 25.19 | Low-medium | The schedule Day field could not be typed to 10–19 | `NumberInput` clamps on blur | Vitest `NumberInput.test.tsx` |
| 25.20 | Low | After an edit, the first run could overlap billed time and be skipped without billing | `ActivateFrom` starts at the first free period | `TestEditContinuesAfterTheBilledPeriod` (weekly → monthly) |
| 25.21 | Low | A once schedule in the past was saved as ended | refused | `TestAOnceScheduleInThePastIsRefused` |
| 25.22 | Low | CRLF bodies were double-spaced | CRLF normalised first | `TestCRLFBodyIsNotDoubleSpaced` |
| 25.23 | Low | TLS "none" with a user name could be saved but never send | refused (except localhost) | `TestSMTPWithoutTLSNeedsNoUser` |
| 25.24 | Low | A subject made long by its placeholders failed the whole invoice run | shortened to 300 bytes | `TestLongRenderedSubjectIsShortened` |
| 25.25 | Low | Outbox: no Sending tab, stopped at 100 | Sending tab, Show more | Vitest |
| 25.26 | Low | The last PDF page (totals, payment, notes) had no DRAFT/VOID/PAID mark | drawn on every page | `TestEveryPageIsWatermarked` (fails before) |
| 25.27 | Low | A statement without a bank account printed invoice wording | statement wording | pdf tests |
| 25.28 | Low | Subtotal/discount could overflow with 100 % discounts | the gross sum is bounded too | `TestHugeDiscountedSubtotalIsRefused` (fails before) |
| 25.29 | Low | Issue used a draft's old date silently | the user picks the old date or today | Vitest `InvoiceDetail.test.tsx` |
| 25.30 | Low | Dashboard debtor bars compared amounts across currencies | scaled per currency | web build |
| 25.31 | Low | ChangePassword could undo an administrator's reset racing it | the user is locked and the hash re-checked | app tests |
| 25.32 | Low | Import: SKUs matched case-sensitively; summary left out products and schedules; ended schedules counted as paused | fixed | app tests |
| 25.33 | — (design) | `settings.export` alone exports every customer and account | kept: the permission's label says so | — |
| 25.34 | High (from the fixes) | Second review: refusing a schedule with no run left in `createSchedule` made any settings file with an ended schedule fail to import | the refusal is in `CreateSchedule` only; an import copies it ended | `TestImportCopiesAnEndedSchedule` |
| 25.35 | Medium-low (from the fixes) | The editor sent the old currency's prices if saved while re-pricing after a customer change | prices cleared at once on a currency change | Vitest |
| 25.36 | Low (from the fixes) | A message reclaimed by another dispatcher got a second sent event; the import's and Business's sequence lock came after the audit head (deadlock with issuing); a server that could never send failed an import; the first unknown login took twice as long; `compose run` created the .env folders as root on Linux | `recordSent` says whether it was ours; the sequence is locked first; such a server is imported switched off; the dummy hash is made at start (`WarmUp`); the folders are made first | `TestImportSwitchesOffAServerThatCannotSend`, app tests |
| 25.37 | — | Docs, VERSION 1.12.1, gate, integration, push, deploy | done | see the M25 evidence below |

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
| 2026-10-15 | The e-mail setup guide is static help text in the web app, beside the form it explains | it decides nothing (the server still tests and stores the settings); one place to read it, where it is needed |
| 2026-10-15 | Light-only enforced at the source: Tailwind `darkMode: 'class'`, no `dark:` classes in `web/src` (a test refuses them) (M13.1) | with the default `media` strategy a phone in dark mode applied stray `dark:` backgrounds under light-theme text; the app never sets the `dark` class, so no future `dark:` class can apply either |
| 2026-10-15 | Which disclaimer a customer's PDFs use is decided by the domain (`domain.UsesOwnDisclaimer`) and sent as `Customer.disclaimerSource` (read-only, optional) (M13.2) | the web app shows it without re-implementing the rule; the existing fallback (`domain.Disclaimer`) already applied on every PDF path, so no behaviour changed |
| 2026-10-15 | `{dd}` placeholder; default number format `{code}/{dd}{mm}-{seq}/{yy}` for new deployments (M13.3) | requested; stored formats are never rewritten, so existing deployments keep numbering as before. A deployment whose business profile was never saved has no stored format, so it switches to the new format at the upgrade to v1.4.0 (accepted: Albie asked for the new format) |
| 2026-10-15 | `make remote-test-int`: integration tests on a Docker host over ssh, shipping the working tree (tracked + untracked, not ignored) as a tarball into a fresh `/data/cactusinv/src`, run in `golang:1.26.9-bookworm` pinned by digest with the host's Docker socket, `TESTCONTAINERS_HOST_OVERRIDE=host.docker.internal` (host-gateway) and `--security-opt label=disable` (SELinux host) | Docker is not run on the dev Mac; uncommitted changes must be testable before a commit; the paths are guarded to stay below `/data/cactusinv/` |
| 2026-10-15 | Settings split into sub-pages from one `settingsNav` list used by the sidebar, the More page and the routes (M13.4) | requested; adding Users or Backup & restore later is one list entry plus a route |
| 2026-10-15 | Every user is an administrator; roles stay under Later (ADR-011, M14 Part A) | the lead accepted the planner's default; a single business needs no role model yet |
| 2026-10-15 | Users are disabled, never deleted; nobody can disable themselves or the last active user (M14) | the audit trail keeps naming who did what; an installation can never lock itself out |
| 2026-10-15 | Users created or reset by an admin, and the default admin, must change their password at first sign-in (server-enforced) (M14) | a password someone else chose (or `admin`) should not outlive its first use |
| 2026-10-15 | Adding, resetting or disabling a user and applying a restore need the actor's current password again (M14) | the most damaging actions must not be one click away for a stolen session |
| 2026-10-15 | Backups are an application-level export (tables as JSON lines in tar.gz), not `mariadb-dump` (ADR-010) | the app image is distroless; the format is versioned, checkable and restorable through the migrations |
| 2026-10-15 | Backups are always encrypted with age: a server X25519 key (`backup_key` secret) for stored backups, a ≥ 16-character passphrase for downloads (ADR-010); not `app_key` | backups hold PII, bank details and password hashes; a key separate from `app_key` can be kept off the host |
| 2026-10-15 | No secrets in backups; an SMTP password that cannot be decrypted after a restore is cleared (ADR-010) | `app_key`, database passwords and session tokens never leave the installation; the SMTP ciphertext only decrypts where its `app_key` lives |
| 2026-10-15 | Invoice PDFs are not in backups; after a restore every invoice is re-exported (ADR-010) | PDFs are derived data; the archive worker re-renders them from the restored rows |
| 2026-10-15 | A restore stages into `<db>_restore`, migrates it to head, then swaps the live data in one locked InnoDB transaction (ADR-010) | the live data is untouched until the backup has been decrypted, verified, loaded and migrated; the swap commits or rolls back as a whole |
| 2026-10-15 | A restore never deletes or rewrites audit entries; it appends the backup's missing entries (verbatim when they chain, re-sealed between markers when not) and a `backup.restored` entry (ADR-009 amended) | the old dump restore silently replaced the audit trail; the trail must only grow |
| 2026-10-15 | Invoice numbers never rewind on a restore (sequence = GREATEST(live, backup)) | numbers already issued and sent to customers must never be issued twice |
| 2026-10-15 | After a restore the dispatcher and schedule runner are held until an admin resumes them | restored outbox rows and schedules may repeat e-mails or invoices already sent after the backup was taken |
| 2026-10-15 | In-app scheduled backups replace the `mariadb-dump` sidecar and CronJob; `.sql.gz` restore is kept one release with a warning and an audit note (ADR-007 amended) | one encrypted mechanism; existing dumps stay restorable while installs upgrade |
| 2026-10-15 | New dependency `filippo.io/age` v1.3.2 (BSD-3-Clause; pulls `filippo.io/hpke` v0.4.0, BSD-3-Clause) for backup encryption (14.11) | a reviewed, specified file-encryption format (X25519 and scrypt recipients, authenticated STREAM payload) that operators can open with the `age` CLI; writing our own would mean inventing a format |
| 2026-10-15 | Audit appends assign `seq` explicitly (last + 1 under the `audit_head` lock) instead of `AUTO_INCREMENT` (14.9, ADR-009 amendment) | InnoDB never gives back an auto-increment value of a rolled-back insert, so any rolled-back append (a failed use case, a deadlock retry) left a gap that `VerifyAuditChain` reports as missing entries; `AppendVerbatim` needs explicit `seq` anyway. No schema change |
| 2026-10-15 | Backup tables are written as parts of at most 8 MiB (`tables/<name>/000001.jsonl` …), every row in exactly its canonical encoding (14.11, ADR-010) | tar needs each entry's size before its data; parts keep memory bounded without temporary files on a read-only container; canonical rows make the digest meaningful |
| 2026-10-15 | `ErrNotConfigured` sentinel for a missing `backup_key`, backups directory or staging database (14.8) | distinct from invalid input: the HTTP adapter can say what to configure |
| 2026-10-15 | Uploaded files are named `…-s0-upload.age` (schema unknown until decrypted); backups from schema < 12 are refused (`MinBackupSchema`) (14.8) | stored names stay generated and strict; schema 12 (v1.4.0) is the last before backups existed |
| 2026-10-15 | A successful stage is not audited; refusals, failures, discards and applies are (14.10) | staging changes no data; auditing it would turn every restore into a fresh installation's trail into a re-seal instead of a verbatim append |
| 2026-10-15 | One stage at a time: staging a new backup (or a failed attempt) replaces any earlier stage; a stage expires after 30 min and housekeeping empties it (14.10) | one staging database per installation |
| 2026-10-15 | A backup whose own audit chain is broken is still restorable: its entries are re-sealed and the break is named in the marker, the preview and `backup.restored` (14.8) | refusing would make a backup useless in a disaster; the break stays visible |
| 2026-10-15 | One backup/restore operation per process at a time (`ErrBusy`); scheduled backups catch up only the latest missed slot, and a failed slot is not retried (the next slot is) (14.10) | bounds memory (scrypt); `backup_runs.slot` keeps replicas from repeating a slot |
| 2026-10-15 | `backup_key` may be an X25519 or a post-quantum hybrid age identity; `backup keygen` generates X25519; the accepted scrypt work factor is capped at 2^18 (14.11) | X25519 opens with every `age` CLI release; a capped work factor stops an upload from demanding gigabytes of memory |
| 2026-10-15 | The stager refuses a staging database name equal to the live one, and a connection that cannot be unlocked after a swap is closed instead of returned to the pool (14.13) | the stager drops every table in the staging database; a pooled connection still holding `LOCK TABLES` would stall the application |
| 2026-10-15 | Review fixes to the restore path (14.13 review): installation-wide `GET_LOCK` for stage/apply/discard; a new stage clears the old one first; panic-safe swap; COMMIT detached from the request; errors carry MariaDB error numbers only; the stager accepts only a `<db>_restore` staging database whose pool is connected to it; decoders require end of input (ADR-010) | an apply in one replica could otherwise copy a staging database another replica was reloading; a panic could return a locked connection to the pool; driver messages can quote PII |
| 2026-10-15 | Downloads are bounded by `ExportTimeout` (default 30 min) instead of first encrypting to a temporary file (ADR-010) | keeps the consistent snapshot from lingering behind a stalled client without doubling disk use |
| 2026-10-15 | Re-seal markers name the exact block they enclose; verbatim appends are impossible for uploaded files (ADR-009 amendment) | a backup's content could otherwise carry a forged end marker or pass forged entries off as the live trail |
| 2026-10-15 | Apply takes the backup `GET_LOCK` on the swap connection itself; stage, discard and housekeeping keep a dedicated lock connection (ADR-010) | a dropped swap connection aborts the swap and frees the lock together; no replica can hold the lock while a swap runs |
| 2026-10-15 | `domain.TruncateUTF8`: truncation cuts at a character boundary within the byte limit (audit summaries, e-mail and schedule errors, held-by/reason, backup `createdBy`, lock name) | the old byte cut could split a UTF-8 character (invalid text under STRICT mode); `createdBy` was cut to 254 characters but validated as 254 bytes, which refused backups by long multi-byte names |
| 2026-10-15 | A restore's COMMIT outcome is decided by its own id, written into `system_state.held_restore_id` inside the swap (migration `00014_restore_outcome`); undeterminable outcomes are audited `backup.restore_outcome_unknown` and returned as `ErrOutcomeUnknown` (ADR-010) | matching on the backup name could report success for a swap that never committed (an earlier unresumed restore of the same file) |
| 2026-10-15 | Staging copies never linger: post-apply discard is detached from the request, locked, and skipped if another stage is recorded; housekeeping empties an unrecorded staging database; backup names are reserved from `Create` to `Commit` (ADR-010) | a cancelled request left a plaintext copy in `<db>_restore`; a discard could drop another replica's stage; same-second uploads failed at commit |
| 2026-10-15 | Export streams from a goroutine through an `io.Pipe`; the response starts once the download is audited and named (14.16) | strict handlers return a body reader; nothing is buffered, and a client that leaves closes the pipe and ends the export |
| 2026-10-15 | Problems: `backup-invalid`/`backup-too-new`/`backup-unsupported` 422, `busy` 409, `not-configured` 503, `restore-outcome-unknown` 500, `payload-too-large` 413 (14.15/14.16) | one problem type per thing the user can act on; the invalid-backup text never says which check failed |
| 2026-10-15 | `GET /backups` lists files without a per-file SHA-256 (14.15) | hashing every file on each listing reads every backup; the SHA-256 is in the `backup.created`/`backup.uploaded` audit entries and in the restore preview |
| 2026-10-15 | The CLI restores as "Operator (CLI)" without a password (`--yes` confirms); a file (or stdin) is uploaded first, so it is audited with its SHA-256 before staging (14.17) | shell access to the server is the authority; the upload keeps the ADR-009 invariant that a supplied file is never appended verbatim |
| 2026-10-15 | Legacy `.sql.gz` restore drops and recreates the database before loading the dump, then restarts the app (migrations) and records `backup.legacy_restored` (14.18) | a dump from schema 12 does not contain the tables of migrations 13–14, which would otherwise already exist and stop the migrations; the whole-database replacement is what the old restore did anyway |
| 2026-10-15 | `deploy.sh`: `backup_key` from `backup keygen` of the image just built (Docker) or of the running pod straight into the Secret (k8s); the staging GRANT and every root SQL go through `MYSQL_PWD` in the container, never a command line; execs that need no input read `/dev/null` (14.18) | no host tool needed for the key; secrets stay off command lines; a script piping into deploy.sh lost its input to `compose exec` (found on the throwaway stack) |
| 2026-10-15 | k8s app memory limit 512 Mi → 768 Mi (request 128 Mi) (14.18) | age scrypt needs ~256 MiB for a passphrase backup, one at a time |
| 2026-10-15 | Before a restore swaps, the live data is stored as a safety backup (kind `prerestore`) when `backup_key` is set up, named in the result and in `backup.restored`; if it fails the restore stops; without the key the preview and the confirmation say no safety backup will be taken (lead's default; ADR-010) | a wrong restore can be undone by restoring the safety backup |
| 2026-10-15 | A failed export after the 200 breaks the connection (`http.ErrAbortHandler`); stage and apply bodies (passphrase, password) are never fingerprinted for Idempotency-Key; long upload/export limits only after sign-in; a failed scheduled backup shows a category (database, disk, key, other), the error only in the log (round-2 review) | a broken download must not look complete; secrets must not be stored as unsalted hashes; internal error text stays in the log |
| 2026-10-15 | Legacy `.sql.gz` restore: read through `backup cat-legacy`, checked (gzip, dump header), safety backup first, only DROP/CREATE DATABASE as root and the dump as the app user; `.age` restores via deploy.sh stage → preview → `RESTORE` → apply by id; staging grant narrowed to `SELECT, INSERT, UPDATE, DELETE, CREATE, DROP, ALTER, INDEX, REFERENCES, LOCK TABLES, TRIGGER`; the k8s app mounts only its secret keys (round-2 review) | least privilege; nothing is dropped before the dump is known good and the current data is saved |
| 2026-10-15 | A browser download is assembled in memory (`Blob`) before saving (accepted for now; README, ADR-010) | invoicing databases stay small; `backup create --out FILE` covers large ones |
| 2026-10-16 | ADR-011 (users) written; ADR-010 amended (must-change gate on backup routes, restore apply rate-limited, "can sign in" = active in the backup) (14.0) | Part A's pending ADR |
| 2026-10-16 | The must-change gate is a strict-server middleware (`sessionGate` → `app.SessionAllows`) on every operation, not in `withAPIGuards`; operation ids compared ignoring case (the generated server passes `ChangePassword`) (14.5) | only the strict server knows the operation; it runs before a handler reads an upload or export body, and a new operation is covered without a list of paths |
| 2026-10-16 | Enabling a user also needs the actor's current password (stricter than the accepted add/reset/disable) (14.3) | re-enabling a disabled account restores access as much as adding one does |
| 2026-10-16 | Upgrade: migration `00015_user_management` flags the default administrator: the user the audit trail records as "default administrator …" (e-mail logins included), and every existing login that is not an e-mail address (14.2, review fix) | before v1.6.0 only the default administrator could have such a login; installations older than the audit trail have no record; its password was chosen by the installation. The flag is set even if its password was already changed (the upgrade cannot tell) |
| 2026-10-16 | `cactusinv admin create\|set-password` do not flag the password; `set-password` ends the user's sessions and clears must-change; both are audited as "Operator (CLI)" (14.3) | shell access is the authority and the host-level way back in; a password chosen at the host need not be changed again |
| 2026-10-16 | Password rate limit (review fix): every route that checks a password (sign-in, `POST /auth/password`, add/reset/disable/enable, restore apply) reserves a slot on arrival in its address's bucket and, signed in, its user's bucket; in flight + failures ≤ 10 per minute; only a wrong password (marked by the response error handler) counts, a right one or a request that never checked one gives the slot back. Argon2 runs at most 4 hashes at once (`argon2.New`). k8s Service `externalTrafficPolicy: Local` (14.5) | checking after the fact let 200 concurrent attempts all reach Argon2 (~12.8 GiB); a missing session must not use up a shared address's limit; NAT/proxies share one address, so a signed-in guesser is also limited per user; Local keeps client addresses (single-node k3s unaffected; multi-node: open the pod's node or use an ingress — README) |
| 2026-10-16 | Sign-in locks the user row in its transaction and starts the session only if the user is still active with the verified hash; add/reset/enable lock the actor's row and refuse a disabled actor; enable ends leftover sessions (`UserRepository.Lock`) (14.3, review fix) | a sign-in racing a password change, reset, CLI set-password or disable could otherwise insert a session after they ended all sessions |
| 2026-10-16 | Password-carrying routes are never fingerprinted for `Idempotency-Key`; a request refused 401/403 frees its key (14.5) | an unsalted hash of a password is guessable; a request held at the password change must run once it is allowed |
| 2026-10-16 | A new password must differ from the current one; passwords are 12–200 characters (`domain.ValidatePassword`) (14.1/14.3) | a temporary password must not survive the forced change |
| 2026-10-16 | `UserRepository.UpdatePassword(id, hash, mustChange)` sets the flag in the same statement (no separate `SetMustChange`) (14.2) | one statement, no window with a new password and the old flag |
| 2026-10-16 | `Login` verifies the password before the disabled flag; session, `last_login_at` and `auth.login` are written in one transaction; the failure reason is only in `auth.login_failed` (14.3) | a disabled account answers exactly like a wrong password, in the same time |
| 2026-10-16 | The e-mail server page warns, it does not refuse, when Gmail or Microsoft 365 will not send as the Send-as address (`domain.EmailSenderWarnings`, M15) | the alias list lives at the provider and cannot be read over SMTP; a verified alias is legitimate, so refusing would block a working setup |
| 2026-10-16 | Only `smtp.gmail.com` / `smtp.googlemail.com` and `smtp.office365.com` / `smtp-mail.outlook.com` are recognised; `smtp-relay.gmail.com` is not (M15) | the Workspace relay may send for the organisation's domains without a per-user alias |
| 2026-10-16 | `POST /settings/email/test` answers `200 {warnings}`; the `204` stays in the contract (M15) | additive for clients that expect no body; the warnings are those of the in-app server that sent, none when the deployment's server sent |
| 2026-10-16 | Warnings are computed in the use case (`app.SMTPView.Warnings`); the settings view warns for the saved settings even while they are disabled, the test result only for the server that actually sent (the in-app one when enabled, else none) (M15, review) | Albie may enable the server later and should see the problem first; a test through the deployment's server says nothing about the in-app one |
| 2026-10-16 | Wording says what the provider does without overclaiming: Gmail "sends as <user> unless <from> is a verified Send mail as address of that account"; Microsoft "may refuse … unless it is the mailbox's own address or <user> has Send As permission"; the page labels the note "For the saved settings:" (M15, review) | known false positives: a Microsoft sign-in name (UPN) that differs from the mailbox's primary address or aliases, and a dotted or googlemail.com spelling of the same Gmail account — a warning, not a refusal, so these cost a needless note only |
| 2026-10-16 | A schedule line's product name, state and current price are read from the product look-up when a schedule is read (`productName`, `productStatus`, `productUnitPrice`), in the use case via `Products.GetMany` — no SQL join, no stored copy (M17.1/17.2) | the stored `description` is empty for product-only lines (the run takes the text from the product) and was being shown as the name; reading through the repository keeps the MariaDB adapter and its queries unchanged |
| 2026-10-16 | `unitPrice` on a schedule line stays the stored override; `productUnitPrice` is display-only and never sent back; the editor keeps an override on save and when the same product is chosen again, and drops it when another product is chosen (M17.3) | the editor used to drop every override on save; a fixed price belongs to the product it was set for |
| 2026-10-16 | Inactive and missing products are shown with "Product no longer available — choose another."; a missing product blocks save in the form (the server refuses it too); an inactive one does not, because the runner still invoices it (M17.3) | stricter server behaviour for inactive products would change running schedules — left as an open question |
| 2026-10-16 | Run times are shown in the zone the server names — `RunPreview.timeZone` for previews, `schedule.recurrence.timeZone` for saved schedules — formatted as "Fri 16 Oct 2026, 09:00 UTC" (en-GB parts, IANA name as the label); a preview without a zone is computed in the business zone, as saving would (M17.1/17.3) | the browser's zone showed 10:00 for a 09:00 UTC run; the preview was computed in UTC while the saved schedule used the business zone |
| 2026-10-16 | `Schedule.lastRunResult` (`ok`/`failed`) from `domain.Schedule.LastRunResult` (failed while consecutive failures are recorded; ok once it ran with none since) — no new column (M17.5) | the list shows the outcome without a rule in React; `failureCount` is reset by every successful run, so it already is the state |
| 2026-10-16 | Schedules list: a table from md up, cards below; Run now and Delete ask in a dialog first; Pause/Resume run at once (M17.5) | Run now raises or e-mails for real and Delete cannot be undone; pausing is reversible |
| 2026-10-16 | Suggested e-mail texts per schedule type are domain templates served by `GET /schedules/defaults` with the placeholders the server fills for each type; a new schedule's form pre-fills them and swaps them when the type changes until the user edits a field; editing a schedule never replaces its texts (M17.6) | the server owns the wording and the placeholders; the user's own words are never overwritten |
| 2026-10-16 | New placeholder `{{date}}`: the invoice's issue date or the statement's as-at date (same value as `{{issue_date}}`) (M17.6) | "as at {{date}}" reads correctly for statements; no other placeholder had the statement date under a fitting name |
| 2026-10-16 | The disclaimer is not copied into a schedule's notes: invoice schedules show, read-only under the notes, the disclaimer every invoice prints (the customer's own or the business's, per `Customer.disclaimerSource`) with a link to edit it there (M17.6) | the disclaimer is added at PDF render time (`domain.Disclaimer`); copying it would print it twice and freeze it against later edits |
| 2026-10-16 | Weekend rule `previous-friday` for monthly, quarterly and yearly schedules: Saturday runs 1 day, Sunday 2 days earlier, literally — the first of a month can run on the last Friday of the previous month or year, and a run can fall before the start date (M17.7) | Albie's rule; the alternative (Monday after) would make e-mails late |
| 2026-10-16 | A moved run keeps its nominal period (`NextOccurrence`, the run-record key, `{month}`/`{period}`); `NextRunAt` is the moved time (M17.7) | the November invoice raised on Friday 30 October is still November's; keying runs on the nominal date keeps "at most one per period" and catch-up unchanged |
| 2026-10-16 | Activating, resuming or editing a schedule starts from the first occurrence whose NOMINAL instant is after now (as before the weekend rule) and sets `NextRunAt` = max(moved run time, now); an occurrence that already ran early is claimed once, hits the (schedule, period) run record and only advances (M17.7, corrected in review 17.9) | choosing by the moved run time skipped an occurrence forever when its moved time had passed but its day had not (rule switched on, schedule created, edited after a failed Friday run, or resumed, on a Saturday) |
| 2026-10-16 | Migration `00016_schedule_weekend_rule`: existing schedules get `none` (unchanged behaviour); new ones default to `previous-friday` when created (or previewed) without the field; an update without it keeps the stored rule; weekly and once are always `none` (M17.7) | existing schedules must not start running on other days after an upgrade; older clients must not reset the rule |
| 2026-10-16 | The day choice is the existing `dayOfMonth` (1 = first day, 0 = last day, N clamped to the month's end); the form shows First day / Last day / Day N (M17.7) | no schema change needed; the clamping rule was already there and is now documented |
| 2026-10-16 | Editing an active schedule, or resuming one paused by `MaxScheduleFailures` failed runs, keeps its unrun pending occurrence: `Schedule.ActivateFrom(pending, now)` searches from the start of pending's billing period (under the edited recurrence) when pending is not after now, and runs it at max(run time, now); a user's pause still skips the periods missed while paused (`PausedByFailures` tells them apart: paused with ≥ `MaxScheduleFailures` failures); an edit keeps the server's run state (`NextOccurrence`, `NextRunAt`, failure count, last error) instead of taking it from the request (17.10, review BLOCKER) | an edit or resume after a failed or overdue run used to drop that period silently (e.g. a run refused for an inactive product, fixed and edited the same morning, lost October); resuming after an auto-pause now also catches up later periods one by one, as after downtime |
| 2026-10-16 | A schedule's run record is unique per billing PERIOD — month (monthly), calendar quarter (quarterly), year (yearly), date (weekly, once) — `schedule_runs.period_key` from `Recurrence.PeriodKey`, migration `00017_schedule_run_period_key` (backfilled from each schedule's frequency; a month already billed twice keeps the later run under its date; primary key `(schedule_id, period_key)`); `period` stays the nominal date shown in the UI and used in e-mail idempotency keys (17.10, review) | moving the day or the start date after a run could bill the same period twice (the key was the nominal date); weekly schedules keep the date as their period, so moving a weekly schedule's weekday can still bill a week twice — accepted, weeks have no natural billing boundary here |
| 2026-10-16 | Upcoming runs start with `NextRunAt` (the actual next run after a weekend move, retry, catch-up or Run now), then the run times of the following occurrences (17.10, review) | the first entry used to show the nominal run time even when the schedule would run at another time |
| 2026-10-16 | A scheduled invoice's issue date = min(its scheduled run date, today in the business time zone) (`Schedule.IssueDate`; lead's decision, 17.9) | Run now never dates an invoice in the future; a catch-up keeps dating each invoice its own (moved) run day, as before |
| 2026-10-16 | The recurring-invoice default texts are the business's own invoice e-mail subject/body (Settings → Business) where set; texts equal to the built-in business defaults count as not set, so the domain template applies; the other types keep the domain templates (`domain.ScheduleEmailsFor`, served by `GET /schedules/defaults`; lead's decision, 17.9) | the business has already said how its invoice e-mails should read; `BusinessProfile.Validate` fills the built-in texts, so "set" has to mean "changed" |
| 2026-10-16 | The runner refuses a line whose product is inactive: the run fails with "product \"<name>\" is no longer available; choose another product in the schedule" (`ErrInactive`), recorded as `lastRunResult` failed; saving with a missing product is refused (unknown product) as before; saving with an inactive one is allowed and warned about in the editor (lead's decision, 17.9) | a deactivated product must not be invoiced silently; refusing the save would block unrelated edits to the schedule |
| 2026-10-16 | `CustomerRepository.GetMany` (query `ListCustomersByIDs`) for the schedule read model (17.9) | one query for every customer on a list page instead of one per schedule |
| 2026-10-16 | The form does not choose a weekend rule for a new schedule: it sends none and shows the rule the server's preview applied (`RunPreview.weekendRule`) (17.9) | the default is the server's |
| 2026-10-16 | Keeping or dropping a line's fixed price when a product is re-picked stays in the editor (17.9 review: justified, not moved) | it is form state — the user replacing a line's product clears what was typed for the old one, as a new line would be; the server stores and validates whatever override is sent and decides nothing from the product change, which it cannot see (lines are replaced as a whole on save) |
| 2026-10-16 | Access groups (ADR-012): permissions belong to groups, users to groups; access = union + implied; a fixed catalogue, view and edit per area; administration (users, groups, backups, restore) only through the built-in Administrators (M18) | Albie's answers 1–2 |
| 2026-10-16 | Upgrade: only the default administrator joins Administrators (fallback: the oldest active user); every other user has no access until put in a group (M18) | Albie's answer 3 |
| 2026-10-16 | A user in no group can sign in but do nothing; the Add user form warns (M18) | Albie's answer 4 |
| 2026-10-16 | Every group and membership change needs the actor's password, takes the Administrators lock and is audited; nobody removes themselves from Administrators (M18) | Albie's answers 5–6; keeps an active administrator without a separate "last administrator" rule |
| 2026-10-16 | Access is checked per API operation in the session middleware; an operation without a rule is refused; a test checks every contract operation has one; currencies, business profile and bank accounts are readable by every signed-in user (M18) | one place to enforce, fail closed; reference data is printed on every invoice anyway |
| 2026-10-16 | Settings → Your account (own password) is open to every user (M18) | a user without administration could otherwise not change their password on a desktop |
| 2026-10-16 | A signed-in actor carries its access into the use cases (`app.Actor.Access`, nil = system/CLI); use cases check what an operation alone cannot: sending a draft (issues it), the dashboard's sections, the outbox list, what a schedule will do (M19) | the operation map cannot see an invoice's state or a schedule's kind |
| 2026-10-16 | A decimal comma is accepted when it cannot be a thousands separator (one comma, no point, not exactly three digits after it) (M19) | comma-region phones have no "." key; "1,500" stays refused rather than guessed |
| 2026-10-16 | PDFs embed DejaVu Sans Condensed (UTF-8) instead of the cp1252 core Helvetica (M19) | customer names in other scripts were printed as dots; +2 MB binary |
| 2026-10-16 | E-mail status changes lock the outbox row; delivery outcomes are recorded on a context that ignores shutdown (15 s) (M19) | a cancel must win or lose cleanly against the dispatcher; an accepted message must never be sent twice |
| 2026-10-17 | Settings import/export is for administrators only, like backups (M22) | the file holds every customer's contact details and the bank account numbers, and an import changes all the settings at once |
| 2026-10-17 | An import merges and never deletes; matches by code (currencies, customers) and by id then currency + account number (bank accounts) (M22) | ids differ between installations; codes and account numbers are what people recognise; deleting from a partial file would be destructive |
| 2026-10-17 | A settings file never holds the SMTP password; an import keeps the stored one (M22) | the password stays encrypted with this installation's app_key |
| 2026-10-17 | Settings files are passphrase-protected by default with the backup sealer (age, scrypt); plain JSON is an explicit opt-out (M22) | the file holds personal data and account numbers; one encryption scheme, openable with `age -d` |
| 2026-10-17 | An imported new schedule is created paused (M22) | moving to a new installation must not start invoicing customers before someone decides it should |
| 2026-10-17 | Folders are mapped, not moved by the app: System Settings store a host folder per container folder and `deploy.sh --install` mounts it (M23) | the container can only write where it is mounted; a mapping is a deploy-time change, so the app shows it as pending until then |
| 2026-10-17 | Session timeouts: an idle timeout (default off) plus a maximum lifetime (default 7 days, as before); both apply at once to every session (M23) | no change for anyone until set; a lowered limit must also end sessions already open |
| 2026-10-17 | Settings access per page and action; Import/Export and System Settings grantable (M24, supersedes "administrators only" for them) | asked for; an import still never exceeds its user's own permissions |

## Later

- Credit notes; partial payments; payment reminders for overdue invoices (would reuse the
  outbox + schedules).
- Customer portal / "view invoice" link.
- OpenTelemetry tracing; Helm chart. (Users with access groups: M18.)

## Open questions

- None outstanding. (Overdue reminders: decided **no** — M8. VAT: configurable, default 0 — M8.)
