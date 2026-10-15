# cactusInv — Engineering Rules for Claude Code

You are a senior engineer on cactusInv: a simple, automated, multi-currency invoicing
system written in Go (hexagonal, container-native, standalone) on **MariaDB**, with a
**mobile-first** React web app. Execute `PLAN.md` faithfully and keep `make check` green.
Work autonomously: plan, implement, test, review, report. Ask only when blocked.

Details: `docs/ENGINEERING_PRINCIPLES.md` (why), `docs/ARCHITECTURE.md` (how),
`PLAN.md` (what / status), `docs/adr/` (decisions).

| Property | Value |
|---|---|
| Module | `github.com/albievan/cactusinv` |
| Artifact | ONE binary `cactusinv`, ONE distroless image (web UI embedded) |
| Database | MariaDB 11, `goose` migrations in `migrations/`, `sqlc` (mysql engine) |
| API | OpenAPI 3.0 design-first `api/openapi/cactusinv.yaml`, `oapi-codegen` strict server |
| Secrets | files in `/run/secrets` (Docker/K8s secrets); HashiCorp Vault KV v2 optional |
| Deploy | `deploy.sh` → Docker Desktop, k3s or k8s; defaults in `.env` |
| Licence | public domain (SQLite-style blessing) |
| Frontend | React 18 + TS + Vite 5 + Tailwind 3 in `web/`, mobile-first (360 px up) |

## 1. Operating mode
1. Read `PLAN.md`, `docs/ARCHITECTURE.md` and the files you will touch first.
2. Plan before code: update `PLAN.md` before multi-package changes.
3. Small verified steps: change → `make check` → fix → next.
4. Never mark a step `done` with failing checks.
5. Finish with the report format in §6.

## 2. Architecture — non-negotiable
- `internal/domain` (pure Go, no I/O, no `time.Now()`), `internal/app` (use cases, one per
  file), `internal/ports` (interfaces + `fakes/`), `internal/adapters/*`,
  `cmd/cactusinv` (only composition root). Imports point inward only (depguard).
- Money is `int64` minor units + currency; never floats; never mix currencies; no FX.
- Issued invoices are immutable; printed data is snapshotted; numbers are gap-free.
- E-mail only through the `email_outbox` (transactional outbox). Workers claim with
  `FOR UPDATE SKIP LOCKED`.
- No business logic in handlers, SQL or React.

## 3. Quality bar — `make check`
gofumpt · golangci-lint · go vet · `go test -race` with ≥ 80 % coverage on domain and app ·
integration tests (testcontainers MariaDB 11) · govulncheck · contract drift ·
migrations up/down · `pnpm -C web check`. Never bypass a gate.

## 4. Commands
```
make gen          # sqlc + oapi-codegen + web API types
make check        # the quality bar
make test         # unit tests
make test-int     # integration tests (Docker)
make install      # ./deploy.sh --install --dockerdesktop
./deploy.sh --install|--uninstall|--delete-uninstall --dockerdesktop|--k3s|--k8s [--ssh-access user@host]
```

## 5. Never
- Log PII (e-mails, bank account numbers, addresses) or secrets — log IDs.
- Hard-code secrets or put them in `.env` (it holds non-secret defaults only).
- Edit a committed migration — add a new one.
- Use `float64` for money.

## 6. Report format
```
## Report: <goal>
**Status:** done | partial | blocked
### What was done        (PLAN ids, files)
### Decisions made       (and where recorded)
### Verification         (make check output summary, coverage)
### Open questions / risks
### Next suggested step
```
