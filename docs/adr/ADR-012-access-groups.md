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

**A fixed catalogue** (`domain.Permissions`), one permission per action (v1.8.0, migration
`00019`; Albie, 2026-10-16: "add a customer but not edit; create an invoice but not edit,
issue or delete"):

| Area | Permissions |
|---|---|
| Dashboard | `dashboard.view` |
| Invoices | `invoices.view`, `invoices.create` (drafts), `invoices.edit` (drafts), `invoices.issue`, `invoices.delete` (drafts), `invoices.send` (e-mail), `invoices.pay`, `invoices.overdue`, `invoices.void` |
| Customers | `customers.view`, `customers.create`, `customers.edit`, `customers.delete` |
| Statements | `statements.view`, `statements.send` |
| Schedules | `schedules.view`, `schedules.create`, `schedules.edit`, `schedules.pause`, `schedules.resume`, `schedules.run` (run now), `schedules.delete` |
| Products | `products.view`, `products.create`, `products.edit`, `products.delete` |
| Outbox | `emails.view`, `emails.cancel`, `emails.retry` |
| Audit log | `audit.view`, `audit.verify` |
| Settings: Business | `settings.business.view`, `settings.business` (change) |
| Settings: E-mail server | `settings.email.view`, `settings.email` (change), `settings.email.test` |
| Settings: Currencies & bank accounts | `settings.currencies.view`, `.create`, `.edit`; `settings.banks.view`, `.create`, `.edit`, `.delete` |
| Settings: Import/Export Settings | `settings.export`, `settings.import` |
| Settings: System Settings | `settings.system.view`, `settings.system.edit` |

A permission implies only what it needs to be *seen*, transitively — never another action
(a test checks this): every action implies its area's view; `invoices.create` and
`invoices.edit` also imply `customers.view` and `products.view` (the editor picks them);
`statements.view` implies `customers.view`; `schedules.view` implies `customers.view` and
`products.view` (its pages show them; v1.7.1). On a page the user may open but not change
(e.g. a customer with `customers.create` only) the form is shown without Save or Delete.

Migration `00019` keeps every group's abilities: `invoices.edit` (which covered create,
edit, issue and delete) became `invoices.create`, `.edit`, `.issue` and `.delete`;
`invoices.status` became `.pay`, `.overdue` and `.void`; `customers.edit`,
`products.edit` and `schedules.edit` gained their create/delete (and pause/run) siblings;
`emails.manage` became `.cancel` and `.retry`; `settings.edit` became the four settings
permissions. Its down migration folds them back (any split permission gives the old one).
Implied permissions are granted with the group, not stored, and the editor
shows them ticked and greyed as "included".

**Amendment (M24, 2026-10-17, migration `00022`)**: "split each access into individual
access". `settings.view` became one view per page (business, e-mail server, currencies, bank
accounts); `settings.currencies` became `.create` and `.edit`, `settings.banks` `.create`,
`.edit` and `.delete`; the test e-mail (`settings.email.test`), resuming schedules
(`schedules.resume`) and verifying the audit log (`audit.verify`) got their own. A group keeps
exactly what it could do; down folds them back. The new Settings pages are grantable:
`settings.export` / `settings.import` and `settings.system.view` / `.edit` (until then
administrators only). An import never grants more than its user's own permissions: each item
it adds or changes needs that action's permission (e.g. `customers.create`), checked in the
dry run too.

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
draft issues it, so it also needs `invoices.issue`; the dashboard leaves out upcoming e-mails
and failures, upcoming runs, recent invoices and the activity feed unless the user may open
them; without `emails.view` the outbox list answers only one customer's statement e-mails
(a statement page's history); creating, changing, resuming or running a schedule needs the
rights of what it does — `invoices.create` and `invoices.issue` (plus `invoices.send` for
auto-send) for invoice
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
