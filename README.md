# cactusInv

A simple, automated, **multi-currency invoicing system** with a **mobile-first** web app.
Schedule invoices to be issued and e-mailed to customers automatically, keep a product
look-up with a price per currency, attach bank accounts to currencies and a currency to each
customer. Runs on **MariaDB**, deploys to **Docker Desktop, k3s or Kubernetes** with one script.

Released into the public domain — see [LICENSE](LICENSE).

## Features

- **Invoices** — draft → issue (gap-free numbers like `CD/1195/26`) → send → paid / void.
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
- **Product look-up** — search and pick products while editing, priced in the invoice currency.
- **Invoice archive** — every issued invoice is exported as a PDF to the invoices volume
  (`<year>/Inv_no_<number>.pdf`) and refreshed when it changes.
- **Scheduled database backups** — daily (configurable), compressed, pruned; one-command restore.
- **Mobile first** — designed for a phone, bottom navigation, large touch targets; scales
  up to a desktop layout. Installable as a home-screen app.

## Quick start (Docker Desktop)

Requirements: Docker Desktop (or Docker Engine with Compose v2).

```bash
git clone https://github.com/albievan/cactusinv.git && cd cactusinv
./deploy.sh --install --dockerdesktop
```

Open <http://localhost:8080> and sign in as **`admin` / `admin`**, then change the password under **More → Change password** (or from the command line):

```bash
echo 'a-long-new-password' | docker compose exec -T app /cactusinv admin set-password admin
```

Test e-mails are captured by Mailpit at <http://localhost:8025>. Set real SMTP settings in
`.env` (and the password in `secrets/smtp_password`) to deliver to customers.

First steps in the app: **More → Settings** (business details, invoice number format and next
number, currencies, bank accounts) → **Customers** → **More → Products** → **Invoices** or
**Schedules**.

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
| `./deploy.sh --backup-now --dockerdesktop` | take a backup immediately |
| `./deploy.sh --restore cactusinv-20261015-020000.sql.gz --dockerdesktop` | restore a backup |

On k3s/k8s the app is exposed on NodePort `K8S_NODE_PORT` (default 30080) and Mailpit on
`K8S_MAILPIT_NODE_PORT` (30025).

## Configuration — `.env`

All defaults live in [`.env`](.env) (no secrets in it):

| Setting | Default | Meaning |
|---|---|---|
| `MARIADB_DATA_DIR` | `./data/mariadb` | database files (Docker) |
| `INVOICES_DIR` | `./data/invoices` | exported invoice PDFs (Docker) |
| `BACKUP_DIR` | `./data/backups` | database backups (Docker) |
| `SECRETS_DIR` | `./secrets` | generated passwords (`db_password`, `db_root_password`, `smtp_password`) |
| `HTTP_PORT` / `MAILPIT_PORT` | `8080` / `8025` | published ports (Docker) |
| `CACTUSINV_DEFAULT_ADMIN_USER` / `_PASSWORD` | `admin` / `admin` | created on first start only |
| `CACTUSINV_SMTP_*` | Mailpit | outgoing mail server |
| `BACKUP_TIMES` / `BACKUP_CRON` / `BACKUP_RETENTION_DAYS` | `02:00` / `0 2 * * *` / `14` | backup schedule (Docker / k8s) and retention |
| `K8S_*` | | namespace, NodePorts, storage class and sizes, registry |
| `TZ` | `Africa/Johannesburg` | time zone for backups; the business time zone is set in the app |

## Development

Requirements: Go 1.26, Node 20 + pnpm 9, Docker (integration tests), `sqlc`, `oapi-codegen`,
`goose`, `golangci-lint`, `gofumpt`, `govulncheck`.

```bash
make help        # list targets
make gen         # regenerate sqlc, OpenAPI server and web API types
make check       # the full quality gate: format, lint, vet, unit tests + coverage,
                 # MariaDB integration tests, govulncheck, contract drift, web checks
pnpm -C web dev  # web app with hot reload, proxying /api to localhost:8080
```

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
deploy, deploy.sh    Docker Compose, k3s/k8s manifests, backups
```

## Operations

- Health: `GET /healthz`, readiness: `GET /readyz`, Prometheus metrics: `GET /metrics`.
- Logs are JSON on stdout and never contain e-mail addresses or bank details.
- CLI inside the container: `/cactusinv migrate up|down`, `/cactusinv admin create <login>`
  and `/cactusinv admin set-password <login>` (password on stdin).
