# ADR-001 — Modular monolith with hexagonal layers

**Status:** accepted · **Date:** 2026-10-15

## Context
cactusInv is explicitly a *simple*, standalone invoice system for one business. It still
needs clean boundaries so that rules about money, numbering and scheduling stay testable
and independent of the database, HTTP and e-mail.

## Decision
One Go module, one binary `cactusinv`, one container image: the HTTP API, the background
workers (schedule runner, e-mail dispatcher, invoice exporter, housekeeping) and the
embedded React UI ship together. Inside, the code is hexagonal
(`domain ← app ← ports ← adapters ← cmd`) and the layering is enforced by `depguard`.

## Consequences
- One thing to deploy, back up and reason about.
- No message broker: the e-mail outbox table is the transactional outbox (ADR-004).
- Workers can be switched off per replica (`CACTUSINV_WORKERS=false`); replicas coordinate
  via row locks.
- If a split is ever needed, the ports already mark the seams.
