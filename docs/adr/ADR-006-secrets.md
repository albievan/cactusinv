# ADR-006 — Secrets from files (Docker/Kubernetes) or HashiCorp Vault

**Status:** accepted (revised 2026-10-15) · **Date:** 2026-10-15

## Context
cactusInv must deploy stand-alone on Docker Desktop, k3s or Kubernetes with one script,
without any external secret store having to exist first.

## Decision
Secrets (`db_password`, `smtp_password`) are read through `ports.Secrets`:
- **file** (default): one file per secret in `CACTUSINV_SECRETS_DIR` (`/run/secrets`). Docker
  Compose mounts them from `${SECRETS_DIR}` (`./secrets`, git-ignored, mode 700); Kubernetes
  mounts the `cactusinv-secrets` Secret. `deploy.sh --install` generates random database
  passwords once; on Kubernetes they are generated straight into the cluster Secret and
  never written to disk locally.
- **vault**: HashiCorp Vault KV v2 over its HTTP API (no SDK), token read from a file.

Session tokens are random and stored only as SHA-256 hashes, so no signing key is needed.

## Consequences
No secret lives in the image, the repository, `.env` or test fixtures. Tests use temporary
files, an `httptest` Vault and in-memory fakes.
