# ADR-011 — Users: administrators who are added, reset and disabled in the app

**Status:** accepted; "one kind of user" superseded by ADR-012 (access groups, v1.7.0) · **Date:** 2026-10-16 (defaults accepted 2026-10-15) · Part A of M14 · Related: ADR-009 (audit), ADR-010 (restore)

## Context
Until v1.5.0 cactusInv had one way to get a user: the default `admin`/`admin` account made
on first start, plus `cactusinv admin create|set-password` on the server. Albie asked for
user management under Settings. The defaults below were proposed by the planner and
accepted by the lead on 2026-10-15.

## Decision

**One kind of user.** Every user is an administrator; there are no roles (PLAN → Later).
A user signs in with a login stored in `users.email`. Logins compare case-insensitively
(the column's `utf8mb4_unicode_ci` collation and the unique key). Logins that already
exist keep working — the default `admin` included — but a user added in the app must be
one valid e-mail address of at most 254 bytes (`domain.ValidateNewLogin`). Passwords are
12–200 characters (`domain.ValidatePassword`), hashed with Argon2id as before.

**Disabled, never deleted.** A user is disabled (`users.disabled_at`) and can be enabled
again; rows are never deleted, so the audit trail always names a real account. Nobody can
disable themselves, and the last active user can never be disabled
(`domain.CheckCanDisable`). The check runs inside the transaction that disables, after
`SELECT id FROM users WHERE disabled_at IS NULL FOR UPDATE` (`UserRepository.LockActive`),
so two administrators disabling each other at the same moment cannot both succeed: the
second waits, re-reads, and is refused. A disabled user cannot sign in, and every session
of theirs is deleted when they are disabled; `Authenticate` refuses a disabled user's
session as well, for the case where one survived.

**Passwords someone else chose do not outlive their first use.** A user added by an
administrator, a password reset by an administrator, and the default administrator all
carry `users.must_change_password`. While it is set the server lets that user's sessions
reach only `GET /auth/me`, `POST /auth/password` and `POST /auth/logout`
(`app.SessionAllows`, enforced by the HTTP adapter for every operation as a strict-server
middleware — the only place the operation is known — the backup and restore routes
included); everything else answers `403 password-change-required`. A request refused this
way (or signed out, 401) does not use up its `Idempotency-Key`. Changing
the password clears the flag. A new password must differ from the current one. On upgrade
(migration `00015_user_management`) the default administrator is flagged, so it must
choose a new password at its next sign-in. It is found two ways: the user the audit trail
records as created as "default administrator …" (whatever `CACTUSINV_DEFAULT_ADMIN_USER`
was, an e-mail address included), and every login that is not an e-mail address — which,
before this release, only the default administrator could have (this covers installations
older than the audit trail, v1.2.0). A default administrator with an e-mail login made
before the audit trail existed cannot be identified and is not flagged.

**Re-authentication for the damaging actions.** Adding a user, resetting someone's
password, disabling and enabling a user (and applying a restore, ADR-010) need the
actor's current password in the request. A wrong one is refused as a validation error on
`currentPassword` and recorded as `user.reauth_failed`. Their bodies are never
fingerprinted for `Idempotency-Key` (an unsalted hash of a password would be guessable).

**Rate limit.** Every route that checks a password (sign-in, changing your password, these
four, applying a restore) takes a slot on arrival in its client address's bucket and, once
signed in, in its user's bucket. Slots in flight plus failures in the last minute never
exceed 10 per bucket, so however many requests arrive at once, at most 10 reach the
password check; the rest answer 429. A right password (or a request that never checked one:
no session, invalid input, unknown user) gives its slot back; a wrong one becomes a failure
for a minute. Separately the Argon2 adapter runs at most 4 hashes at once (64 MiB each), so
attempts from many addresses cannot exhaust memory. The address is the TCP peer: behind NAT
or a proxy many people share one bucket (the per-user bucket limits a signed-in guesser on
their own); the k3s/k8s Service uses `externalTrafficPolicy: Local` to keep client
addresses, at the price of the NodePort answering only on the node running the app.

**Sessions.** Disabling a user or resetting their password deletes all of their sessions;
enabling one deletes any left over. A sign-in checks the password before its transaction;
inside it, it locks the user row (`UserRepository.Lock`, `FOR UPDATE`) and starts the
session only if the user is still active with the password hash it verified — a password
change, reset or disable that committed meanwhile makes it fail (generic answer, reason
audited). Adding, resetting and enabling lock the actor's row and refuse an actor disabled
since re-authenticating.
Changing your own password deletes your other sessions and keeps the one you used.
Nobody can reset their own password through the reset endpoint (that is what changing
your password is for).

**Sign-in.** Failures are one generic `401` whatever the reason; the reason (unknown
login, wrong password, disabled user) is recorded in the `auth.login_failed` audit entry.
A successful sign-in records `users.last_login_at`.

**Audit** (ADR-009): `user.created`, `user.password_reset`, `user.disabled`,
`user.enabled`, `user.password_changed`, `user.reauth_failed`, `auth.login_failed` (with
its reason).

**Recovery stays on the host.** `cactusinv admin set-password <login>` (password on stdin)
still works for any user and is the way back in when every password is forgotten: shell
access to the server is the authority, so the password it sets is not flagged
must-change, and it deletes that user's sessions. `cactusinv admin create` likewise.

## Consequences
- An installation can never lock itself out through the app: there is always one active
  user, and a must-change user can always change the password and continue.
- After the upgrade to v1.6.0 the default `admin` is asked for a new password at its next
  sign-in, even if its password was changed before (the upgrade cannot tell).
- A restore (ADR-010) brings back the users, their flags and their disabled state as they
  were in the backup; the preview warns when your account is not an active user there.
- Roles, OIDC and per-user permissions remain out of scope. (Access groups came later: ADR-012.)
