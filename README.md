# cactusInv

A simple, automated, **multi-currency invoicing system** with a **mobile-first** web app.
Schedule invoices to be issued and e-mailed to customers automatically, keep a product
look-up with a price per currency, attach bank accounts to currencies and a currency to each
customer. Runs on **MariaDB**, deploys to **Docker Desktop, k3s or Kubernetes** with one script.

Released into the public domain — see [LICENSE](LICENSE).

## Features

- **Invoices** — draft → issue (gap-free numbers like `CD/2108-1195/26`: customer code, issue
  day and month, sequence, year — the format is yours to set) → send → paid / void.
  Classic business-invoice PDF layout (header band, bill-to/ship-to, line table, bank details,
  totals box), with DRAFT/VOID watermarks.
- **Automated e-mail** — send now, schedule a send for later, or let a **recurring schedule**
  (weekly, monthly, quarterly, yearly) issue invoices and, if you opt in, e-mail them.
  Outstanding invoices are never e-mailed automatically. Reliable
  transactional outbox with retries; failed sends visible in the Outbox.
- **Multi-currency** — every customer is billed in one currency; every currency has its own
  bank accounts (one default, printed on its invoices); products carry an explicit price per
  currency. No exchange-rate guessing.
- **VAT** — set a default VAT % for the business (0 by default) and override it per invoice or
  schedule. At 0 % nothing about VAT appears on the invoice.
- **Product look-up** — every invoice line is picked from a searchable product grid (no free
  text); each product has a unit cost per currency.
- **Stages** — draft → issued → e-mailed → paid, or overdue / void, shown as a progress tracker;
  mark invoices paid, overdue or void.
- **Statements** — a Statements menu with every customer's balance and aging; open any
  customer's full statement, download the PDF, e-mail one or many statements at once, or
  e-mail them on a schedule.
- **E-mail server in the app** — Settings → E-mail server, Gmail by default (with an app
  password), test button, and a **Setup guide** with step-by-step instructions for Gmail,
  Outlook / Microsoft 365 and other servers; the password is stored encrypted. cactusInv
  sends From the **Send as** address, but the provider decides the sender it shows: Gmail
  sends as the signed-in account unless Send as is a verified alias (Gmail → Settings →
  Accounts → Send mail as), and Microsoft 365 may refuse unless it is the mailbox's own
  address or the mailbox has Send As permission for it. The page and the test result warn when that applies.
- **Disclaimer** — printed on every PDF and read fresh each time a PDF is produced; a
  customer without its own disclaimer gets the business disclaimer, and the customer form
  says which one its PDFs use.
- **Audit trail** — every change and sign-in with who did it; append-only (the database
  refuses edits and deletes) and hash-chained, with a one-click integrity check.
- **Dashboard** — outstanding and overdue per currency, invoiced vs paid over 12 months, who
  owes the most, what is scheduled next and recent activity.
- **Recurring invoice workflows** — raise a predefined invoice on a schedule, then e-mail it
  straight away or a set number of days later. Lines use the product's current price in the
  customer's currency unless a fixed price is set; run times are shown in the schedule's time
  zone, picked from a list in the editor (it starts as the business time zone), e.g. "Fri 16 Oct 2026,
  08:00 Africa/Johannesburg"; when it differs from your device the editor shows the first run in
  your device's time too. Until the business profile is saved, the business time zone is `TZ`.
  The Schedules page lists each schedule's type, last run (and whether it failed) and next run,
  with Edit, Pause/Resume, Run now and Delete beside it; Run now and Delete ask first.
  Monthly, quarterly and yearly schedules run on the first day, the last day or a chosen day
  (a day past the month's end means its last day); by default a run that falls on a Saturday or
  Sunday is sent on the Friday before — still for its own month (schedules made before v1.6.2
  keep running on the day itself until you change them). A new schedule starts with a suggested
  e-mail subject and body for its type; invoice schedules show the disclaimer every invoice
  prints and where to change it.
  Billing periods count from the start date (a quarterly schedule starting in February bills
  February–April, May–July, …), and no stretch of time is billed twice: after you change a
  schedule's day, frequency or start date it continues in the next period not yet billed (the
  editor's run list already shows this). Resuming a schedule whose last run had failed runs that
  period now; a schedule that was running fine and was paused skips the periods missed.
- **Invoice archive** — every issued invoice is exported as a PDF to the invoices volume
  (`<year>/Inv_no_<number>.pdf`) and refreshed when it changes.
- **Encrypted backups and restore** — the app backs itself up daily (configurable), encrypted
  with [age](https://age-encryption.org); download a backup protected by your passphrase; restore
  from Settings → Backup & restore with a preview first. The audit log is never rewritten.
- **Import/Export Settings** — Settings → Import/Export Settings (administrators): export the
  business details, e-mail server, currencies, bank accounts and customers to one JSON file, and
  import such a file into this or another installation. An import is checked first (what is new,
  changed or unchanged, per section you choose), then applied all or nothing; it never deletes,
  the next invoice number only moves forward, and the e-mail server password is never in the file.
- **Users and access groups** — Settings → Users & groups: add users, reset a password,
  disable and enable; create groups, choose what each group may do — each action on its own
  (e.g. add customers but not edit or delete them; create invoices but not edit, issue or
  delete them; mark paid but not void) — and who is in it. Only Administrators manage users, groups, backups and restore. Users are
  never deleted, and a password someone else chose must be changed at the first sign-in.
- **Typing amounts** — use a point or, on a phone set to a comma region, a comma for decimals
  ("192,94"); "1,500" is refused as ambiguous. PDFs print names in Latin, Greek and Cyrillic
  scripts as typed.
- **Mobile first** — designed for a phone, bottom navigation, large touch targets; scales
  up to a desktop layout. Installable as a home-screen app.

## Quick start (Docker Desktop)

Requirements: Docker Desktop (or Docker Engine with Compose v2).

```bash
git clone https://github.com/albievan/cactusinv.git && cd cactusinv
./deploy.sh --install --dockerdesktop
```

Open <http://localhost:8080> and sign in as **`admin` / `admin`**. You are asked to choose a
new password straight away; nothing else works until you have (the server enforces it). Then
add the people who need to sign in under **Settings → Users**.

Upgrading from v1.5.0 or earlier: the default administrator (`admin`, or whatever
`CACTUSINV_DEFAULT_ADMIN_USER` was) is asked for a new password at its next sign-in, even if
it was changed before (the upgrade cannot tell). Locked out? Set a password on
the server — it works for any user and ends that user's sessions:

```bash
echo 'a-long-new-password' | docker compose exec -T app /cactusinv admin set-password admin
```

Test e-mails are captured by Mailpit at <http://localhost:8025>. Set real SMTP settings in
`.env` (and the password in `secrets/smtp_password`) to deliver to customers.

First steps in the app: **Settings → Business** (business details, invoice number format and
next number), **Settings → E-mail server**, **Settings → Currencies & bank accounts** (on a phone:
**More → Settings**) → **Customers** → **More → Products** → **Invoices** or **Schedules**.

Invoice number format placeholders: `{code}` customer code, `{seq}` the gap-free sequence
(required), `{dd}` and `{mm}` the issue day and month, `{yy}` or `{yyyy}` the year. New
installs start with `{code}/{dd}{mm}-{seq}/{yy}` (`CD/2108-1195/26` for 21 August 2026);
an install that already saved a format keeps it.

Every placeholder for e-mail subjects and bodies (`{{invoice_number}}`, `{{total}}`, …), invoice
numbers and recurring-invoice line descriptions (`{month}`, `{period}`, …) is listed with
examples in [docs/PLACEHOLDERS.md](docs/PLACEHOLDERS.md).

## Deploying — `deploy.sh`

```
./deploy.sh ACTION TARGET [--ssh-access user@host] [--yes]

ACTION   --install | --uninstall | --delete-uninstall | --status | --backup-now | --restore FILE
TARGET   --dockerdesktop | --k3s | --k8s
```

| Example | What it does |
|---|---|
| `./deploy.sh --install --dockerdesktop` | build and run locally with Docker Compose |
| `./deploy.sh --install --dockerdesktop --ssh-access root@192.0.2.10` | copy to `/opt/cactusinv` on the host and run it there with Docker |
| `./deploy.sh --install --k3s --ssh-access root@192.0.2.10` | build for the node's CPU, import into k3s, apply manifests (no registry needed) |
| `./deploy.sh --install --k8s` | push the image to `K8S_REGISTRY` and apply to the current `kubectl` context |
| `./deploy.sh --uninstall --k3s …` | remove the app, **keep** database, invoices and backups |
| `./deploy.sh --delete-uninstall --dockerdesktop` | remove the app **and all data** (asks to confirm; `--yes` to skip) |
| `./deploy.sh --backup-now --dockerdesktop` | take an encrypted backup now (stored in `BACKUP_DIR`) |
| `./deploy.sh --restore cactusinv-20261015T000000Z-s14-scheduled.age --dockerdesktop` | restore a stored backup (or a path to an `.age` file) |
| `./deploy.sh --restore cactusinv-20261001-020000.sql.gz --dockerdesktop` | restore an old plaintext dump (whole database replaced; with a warning) |

On k3s the SSH user must be root or have passwordless `sudo` (`k3s ctr` and `k3s kubectl`
need root); the image is imported for the node's CPU without attestation manifests, checked
on the node, and run with `imagePullPolicy: Never`. If a rollout is not ready within five
minutes the script prints the pods, their events and the last log lines before it stops.
A volume claim that already exists keeps its size (claims cannot shrink); the `K8S_*_STORAGE`
sizes apply to new installs. Pods do not mount a Kubernetes service-account token (none of
them uses the API; on k3s the token's mount point would fall inside the read-only
`/run/secrets` volume and MariaDB would not start).

After a first install on k3s/k8s, sign in with `admin` / `admin` (you are asked to change it)
and copy the generated backup key off the cluster:

```
kubectl -n cactusinv get secret cactusinv-secrets -o jsonpath='{.data.backup_key}' | base64 -d
```

On k3s/k8s the app is exposed on NodePort `K8S_NODE_PORT` (default 30080) and Mailpit on
`K8S_MAILPIT_NODE_PORT` (30025). The app's Service uses `externalTrafficPolicy: Local`, so
the sign-in rate limit sees each client's own address; the port then answers only on the
node that runs the app (on a single-node k3s, that node). On a multi-node cluster open that
node (`kubectl get pod -o wide`) or put an ingress in front. Behind NAT or a proxy, clients
share one address and so share its limit of 10 failed password attempts a minute; a
signed-in user also has a limit of their own.

## Configuration — `.env`

All defaults live in [`.env`](.env) (no secrets in it):

| Setting | Default | Meaning |
|---|---|---|
| `MARIADB_DATA_DIR` | `./data/mariadb` | database files (Docker) |
| `INVOICES_DIR` | `./data/invoices` | exported invoice PDFs (Docker) |
| `BACKUP_DIR` | `./data/backups` | encrypted backups (Docker) |
| `SECRETS_DIR` | `./secrets` | generated secrets (`db_password`, `db_root_password`, `smtp_password`, `app_key`, `backup_key`) |
| `HTTP_PORT` / `MAILPIT_PORT` | `8080` / `8025` | published ports (Docker) |
| `CACTUSINV_DEFAULT_ADMIN_USER` / `_PASSWORD` | `admin` / `admin` | created on first start only |
| `CACTUSINV_SMTP_*` | Mailpit | outgoing mail server |
| `BACKUP_TIMES` / `BACKUP_RETENTION_DAYS` | `02:00` / `14` | daily backup times (in `TZ`) and how long backups are kept (0 = always) |
| `BACKUP_MAX_UPLOAD_MB` | `256` | largest backup file that can be uploaded for a restore |
| `K8S_*` | | namespace, NodePorts, storage class and sizes, registry |
| `TZ` | `Africa/Johannesburg` | time zone for backups, and the business time zone until it is saved in the app (Settings → Business) |

## Users

Settings → Users & groups (on a phone: More → Users & groups; Administrators only) lists
everyone who can sign in, with their groups, status and last sign-in. What a user may do
comes from their **groups** (ADR-012):

- **Groups tab** — create a group, tick what it may do (view and edit per area: Dashboard;
  Invoices — view, edit, e-mail, mark paid/overdue/void; Customers; Statements — view, send;
  Schedules; Products; Outbox; Audit log; Settings) and who is in it; change or delete it
  later. Permissions a ticked one needs (e.g. seeing invoices, to e-mail them) are included
  and shown greyed. A change applies at once.
- **Administrators** is built in: it can do everything, including users, groups, backups and
  restore. Only its members can change, and you cannot remove yourself from it.
- A user in **no group** can sign in but sees nothing (the Add user form warns). **Access…**
  on a user's row sets their groups.
- Every group and membership change asks for your own password and is in the audit log.
- Upgrading to v1.7.0: only the default administrator (`admin`) stays an administrator;
  everyone else has no access until you put them in a group.

- **Add user** — the login is the person's e-mail address; you choose a first password and
  give it to them. Existing logins such as `admin` keep working.
- **Reset password** — gives someone a temporary password and signs them out everywhere.
- **Disable / Enable** — a disabled user cannot sign in and is signed out at once; nothing they
  did is removed. Users are never deleted. You cannot disable yourself or the last active user.
- Adding, resetting, disabling, enabling and every group change ask for **your own password** again; wrong
  attempts are recorded in the audit log and limited to 10 a minute per address (shared with
  sign-in).
- A password chosen by someone else (a new user, a reset, the default `admin`) must be changed
  at the next sign-in: until then only the change-password screen and Sign out work.
- Changing your own password signs out your other sessions (other browsers and phones).
- A failed sign-in always says the same thing; the reason (unknown login, wrong password,
  disabled user) is in the audit log.
- On the server, `cactusinv admin set-password <login>` (password on stdin) is the way back in
  if every password is forgotten; `cactusinv admin create <login>` adds an administrator.

## Backup and restore

The app takes the backups itself (ADR-010): every table except sessions and other transient
state, as one consistent snapshot, compressed and **encrypted** with
[age](https://age-encryption.org). Invoice PDFs are not in backups; they are recreated after a
restore. No secret is in a backup.

- **Stored and scheduled backups** are encrypted to the server key `backup_key`, which
  `deploy.sh --install` generates in `SECRETS_DIR` (on k3s/k8s: in the `cactusinv-secrets`
  Secret). **Copy `backup_key` off the machine and keep it apart from the backups**: without it
  the stored backups cannot be restored, and a key kept next to its backups protects nothing.
  A stored backup opens without the app: `age -d -i secrets/backup_key FILE.age | tar -xz`
  (JSON lines per table).
- **Download a backup** (Settings → Backup & restore) makes a fresh backup encrypted with a
  passphrase of at least 16 characters; nothing is stored on the server. The passphrase cannot
  be recovered.
- **Restore** (Settings → Backup & restore → Restore… on a stored backup, or Restore from file
  for a downloaded one): the backup is decrypted, checked and loaded into a separate staging
  database first, and a preview shows what restoring changes — invoices created since the
  backup, whether your account is an active user in it, warnings. Applying it needs your password and the word
  `RESTORE`; it replaces the data in one transaction, never reuses invoice numbers, signs
  everyone out, and **pauses automatic e-mails and schedules** until you check the Outbox and
  resume (a banner on every page). With `backup_key` set up, the current data is saved first
  as a **safety backup** ("Before a restore"), so a wrong restore can be undone. The audit log
  is never rewritten: the backup's entries are added to it.
- From the command line (in the app container): `cactusinv backup create`, `backup list`,
  `backup create --out FILE --passphrase-file P`, `backup restore NAME|FILE|- [--yes]`
  (shows what restoring does and asks for `RESTORE`), `backup stage …` / `backup apply ID
  --yes` / `backup discard ID`, `backup note-legacy-restore FILE`, `backup keygen`.
- Old plaintext `.sql.gz` dumps of the retired backup container are listed and pruned with the
  rest; restore one with `deploy.sh --restore NAME` (this release only). The script reads it
  from the backups volume through the app (`cactusinv backup cat-legacy NAME`), checks it,
  takes a safety backup, asks for `RESTORE`, and loads it as the application user.
- A download in the browser is put together in memory before it is saved; for a very large
  database use `cactusinv backup create --out FILE` instead.

## Development

Requirements: Go 1.26, Node 20 + pnpm 9, Docker (integration tests), `sqlc`, `oapi-codegen`,
`goose`, `golangci-lint`, `gofumpt`, `govulncheck`.

```bash
make help        # list targets
make gen         # regenerate sqlc, OpenAPI server and web API types
make check       # the full quality gate: format, lint, vet, unit tests + coverage,
                 # MariaDB integration tests, govulncheck, contract drift, web checks
pnpm -C web dev  # web app with hot reload, proxying /api to localhost:8080
make remote-test-int                      # integration tests on a Docker host over ssh
make remote-test-int REMOTE=user@host     # REMOTE is required; runs in /data/cactusinv/src there
```

`make remote-test-int` is for machines without Docker: it copies the working tree, including
uncommitted changes, to a fresh `REMOTE_DIR` (below `/data/cactusinv/`) and runs the MariaDB
integration tests there in the pinned `golang:1.26.9` image with the host's Docker socket.

Read [PLAN.md](PLAN.md), [docs/ENGINEERING_PRINCIPLES.md](docs/ENGINEERING_PRINCIPLES.md),
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/adr](docs/adr) before changing code.

### Layout

```
cmd/cactusinv        composition root (serve, migrate, admin, healthcheck)
internal/domain      money, currencies, invoices, terms, numbering, schedules, outbox — pure Go
internal/app         use cases
internal/ports       interfaces (+ in-memory fakes)
internal/adapters    mariadb (sqlc), http (OpenAPI), pdf, smtp, archive, secrets, worker
migrations           goose SQL migrations (embedded)
api/openapi          the API contract
web                  React + TypeScript + Vite + Tailwind (embedded in the binary)
deploy, deploy.sh    Docker Compose, k3s/k8s manifests, MariaDB init (restore staging database)
```

## Operations

- Health: `GET /healthz`, readiness: `GET /readyz`, Prometheus metrics: `GET /metrics`.
- Logs are JSON on stdout and never contain e-mail addresses or bank details.
- CLI inside the container: `/cactusinv migrate up|down`, `/cactusinv admin create <login>`
  and `/cactusinv admin set-password <login>` (password on stdin).
