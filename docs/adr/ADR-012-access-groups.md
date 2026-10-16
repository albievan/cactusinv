# ADR-012 — Access groups: what each user may do

**Status:** accepted · **Date:** 2026-10-16 · M18 · Amends ADR-011 ("one kind of user") · Related: ADR-009 (audit), ADR-010 (restore)

## Context
Until v1.6.2 every user could do everything (ADR-011). Albie asked to "create groups and
assign access to the group or remove access from a group" on the Users page, and decided:
only the built-in Administrators manage users, groups, backups and restore; access is view
and edit per area; on upgrade only the default administrator keeps full access and
everyone else starts with none; a user in no group has no access (the form warns); every
group and membership change needs the actor's password; nobody removes themselves from
Administrators.

## Decision

**Groups hold permissions; users hold groups.** A user's access is the union of the
permissions of the groups they are in, plus what those imply. There are no per-user
permissions and no deny rules. A user in no group can sign in, read their own account and
change their password, and nothing else.

**A fixed catalogue** (`domain.Permissions`), view and edit per area:

| Area | Permissions |
|---|---|
| Dashboard | `dashboard.view` |
| Invoices | `invoices.view`, `invoices.edit` (create, edit, issue, delete drafts), `invoices.send` (e-mail), `invoices.status` (paid, overdue, void) |
| Customers | `customers.view`, `customers.edit` |
| Statements | `statements.view`, `statements.send` |
| Schedules | `schedules.view`, `schedules.edit` (create, edit, pause, resume, run, delete) |
| Products | `products.view`, `products.edit` |
| Outbox | `emails.view`, `emails.manage` (cancel, retry) |
| Audit log | `audit.view` |
| Settings | `settings.view`, `settings.edit` (business, e-mail server, currencies, bank accounts) |

A permission implies what it needs to be usable, transitively: every edit/send/status
permission implies its area's view; `invoices.edit` and `schedules.edit` also imply
`customers.view` and `products.view` (the editors pick them); `statements.view` implies
`customers.view`; `schedules.view` implies `customers.view` and `products.view` (its pages
show them; v1.7.1). Implied permissions are granted with the group, not stored, and the editor
shows them ticked and greyed as "included".

Every signed-in user may read the reference data every page needs: currencies, the
business profile and bank accounts (all printed on every invoice anyway).

**Administrators** (`access_groups.id = 'administrators'`, `builtin`) has every permission
and, alone, `administer`: users, groups, backups, restore and resuming automation. It
cannot be renamed, given or stripped of permissions, or deleted; only its members change.
Nobody removes themselves from it (as nobody disables themselves), so it always keeps an
active member: the administrator making the change.

**Enforcement is per API operation**, in the same strict-server middleware that enforces a
forced password change (`app.SessionAllows`): `app.operationAccess` maps every OpenAPI
`operationId` to the permissions that allow it (any one is enough). An operation without a
rule is refused for everybody; an HTTP test checks that every operation in the contract has
one. A refusal is `403 no-access`. Access is read from the database on every request, so a
group change applies at once, without signing anyone out. The web app hides menus, pages
and buttons the user may not use (`useCan`), but the server is the authority.

**Checks the operation map cannot make** (v1.7.1): the signed-in user's access travels in
the actor (`app.Actor.Access`; nil for the system workers and the command line). Sending a
draft issues it, so it also needs `invoices.edit`; the dashboard leaves out upcoming e-mails
and failures, upcoming runs, recent invoices and the activity feed unless the user may open
them; without `emails.view` the outbox list answers only one customer's statement e-mails
(a statement page's history); creating, changing, resuming or running a schedule needs the
rights of what it does — `invoices.edit` (plus `invoices.send` for auto-send) for invoice
schedules, `invoices.send` for e-mail schedules of invoices, `statements.send` for statement
schedules.

**Changes.** Creating, changing and deleting a group, and setting a user's groups (also when
adding a user), need the actor's current password (the sign-in rate limit applies; the
bodies are never fingerprinted for Idempotency-Key). Inside the transaction the active
members of Administrators are locked (`GroupRepository.LockActiveAdministrators`, `FOR
UPDATE`) and the actor must still be one (`domain.CheckGroupChange`): two administrators
removing each other at the same moment cannot both succeed. User management (add, reset,
disable, enable) takes the same lock. Every change is audited: `group.created`,
`group.updated` (what was added and removed: access and members), `group.deleted` (who lost
access), `user.groups_changed` (new and previous groups), `user.created` (with its groups).

**Upgrade** (migration `00018_access_groups`): the tables `access_groups`,
`access_group_permissions` and `access_group_members` are created and Administrators is
seeded with the default administrator only — found as in `00015` (the user the audit trail
records as "default administrator …", or a login that is not an e-mail address). If none
is found, or the one found is disabled, the oldest active user joins, so an installation
never ends up without an active administrator. Everyone else has no access until an
administrator puts them in a group. `cactusinv admin create` (shell access is the
authority) and the default administrator made on first start join Administrators.

## Consequences
- Upgrading to v1.7.0 removes the access of every user except the default administrator
  (Albie's decision); they must be put in groups.
- Backups include the new tables; restoring an older backup migrates it, so its users
  get the same treatment as an upgrade.
- A new API operation needs an access rule, or nobody can use it (the contract test fails
  first).
- Out of scope: per-user permissions, deny rules, per-customer or per-currency scopes, OIDC.
