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
  totals box), with a DRAFT, PAID or VOID mark on every page. Issuing a draft dated in the past
  asks whether to keep its date or issue it dated today (the due date follows the date used).
  Lists page through everything ("Show more"), however many invoices, customers or products.
- **Automated e-mail** — send now, schedule a send for later, or let a **recurring schedule**
  (weekly, monthly, quarterly, yearly) issue invoices and, if you opt in, e-mail them.
  Outstanding invoices are never e-mailed automatically. Reliable
  transactional outbox with retries; the Outbox shows what is queued, sending, sent and failed.
  A message the mail server has accepted is never sent again, even if recording it fails.
- **Multi-currency** — every customer is billed in one currency; every currency has its own
  bank accounts (one default, printed on its invoices); products carry an explicit price per
  currency. No exchange-rate guessing. A customer pinned to a bank account that is later
  deactivated is invoiced with the currency's default account; a customer's currency cannot
  change while they have unpaid invoices.
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
- **Import/Export Settings** — Settings → Import/Export Settings: export the
  business details, e-mail server, currencies, bank accounts, products, customers and schedules
  to one JSON file, protected with a passphrase unless you choose readable JSON, and import such a
  file into this or another installation. An import is checked first (what is new, changed or
  unchanged, per section you choose), then applied all or nothing. It never deletes, new
  schedules arrive paused, the next invoice number only moves forward, and the e-mail server
  password is never in the file.
- **System Settings** — Settings → System Settings: sign users out after a
  time without activity and end every sign-in after a maximum time (applies at once), and map
  the container's backup and invoice export folders to host folders, e.g. a NAS share. A new
  mapping is used by the next `./deploy.sh --install --dockerdesktop`, which copies the
  existing files into an empty new folder. On Kubernetes the folders are volume claims. It also
  shows the running version, and **Check for updates** says whether a newer one is published and
  the command that upgrades to it (`./deploy.sh --upgrade`; see [Upgrading](#upgrading)).
- **Users and access groups** — Settings → Users & groups: add users, reset a password,
  disable and enable; create groups, choose what each group may do — each action on its own
  (e.g. add customers but not edit or delete them; create invoices but not edit, issue or
  delete them; mark paid but not void) — and who is in it. Only Administrators manage users, groups, backups and restore. Users are
  never deleted, and a password someone else chose must be changed at the first sign-in.
- **Typing amounts** — use a point or, on a phone set to a comma region, a comma for decimals
  ("192,94"); "1,500" is refused as ambiguous. PDFs print names in Latin, Greek and Cyrillic
  scripts as typed.
- **Mobile first** — designed for a phone, bottom navigation, large touch targets; scales
  up to a desktop layout. Installable as a home-screen app. Built on Bootstrap 5.3 with
  Bootstrap Icons: compact buttons show an icon, with its name as a tooltip and for screen readers.

## Quick start (Docker Desktop)

Requirements: Docker Desktop (or Docker Engine with Compose v2).

```bash
git clone https://github.com/albievan/cactusinv.git && cd cactusinv
./deploy.sh --install --dockerdesktop
```

The data folders in `.env` default to `/data/cactusinv/…`. Where `/data` cannot be created (on a
Mac the disk root is read-only) or you are not root, set `DATA_DIR`, `MARIADB_DATA_DIR`,
`INVOICES_DIR`, `BACKUP_DIR` and `SECRETS_DIR` to a folder you can write, e.g. `./data/…` and
`./secrets` (both git-ignored), **before** the first install. See
[Configuration](#configuration--env).

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

Test e-mails are captured by Mailpit at <http://localhost:8025>. To deliver to customers, set the real
mail server in Settings → E-mail server (or the fallback in `.env`, with its password in
`${SECRETS_DIR}/smtp_password`).

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

ACTION   --install | --upgrade [--to REF] | --version | --uninstall | --delete-uninstall | --status
         | --backup-now | --restore FILE
TARGET   --dockerdesktop | --k3s | --k8s
```

| Example | What it does |
|---|---|
| `./deploy.sh --install --dockerdesktop` | build and run locally with Docker Compose |
| `./deploy.sh --install --dockerdesktop --ssh-access root@192.0.2.10` | copy to `/opt/cactusinv` on the host and run it there with Docker |
| `./deploy.sh --install --k3s --ssh-access root@192.0.2.10` | build for the node's CPU, import into k3s, apply manifests (no registry needed) |
| `./deploy.sh --install --k8s` | push the image to `K8S_REGISTRY` and apply to the current `kubectl` context |
| `./deploy.sh --upgrade --dockerdesktop` | upgrade to the newest version (see [Upgrading](#upgrading)) |
| `./deploy.sh --upgrade --to v1.13.0 --k3s --ssh-access root@192.0.2.10` | upgrade to a given tag, branch or commit |
| `./deploy.sh --version --dockerdesktop` | the running version and database schema |
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

## Upgrading

The app runs without rights over Docker or the cluster, so it cannot replace itself; Settings →
System Settings → **Check for updates** says when a newer version is published and which command
installs it. Run it in this folder (the git checkout you install from), for the same target and
`--ssh-access` you installed with:

```
./deploy.sh --upgrade --dockerdesktop
```

1. It stops at once, changing nothing, if tracked files have local changes, the checkout is not
   on a branch, or cactusInv is not running there.
2. It fetches the branch's upstream (or `--to REF`) and only moves **forward**: an older version
   is refused (going back to one needs a restore of a backup it made).
3. It shows the running and the new version, the commits in between and whether the database
   changes, and asks you to type `upgrade` (`--yes` skips the question).
4. It takes a backup first; if that fails nothing is changed.
5. It moves the checkout forward and installs with the **new** version's `deploy.sh`, then checks
   that the new version is the one running. Nobody is signed out; the app is unavailable for a
   minute or two while it restarts, and the new version updates the database as it starts.
6. If the install or the check fails, it goes back: the new version undoes its own database
   changes (`cactusinv migrate down-to N`), the checkout returns to the old commit and the old
   version is installed again, with the data as it was. It names the backup taken in step 4.
   On k3s/k8s the database changes can only be undone while the new version's pod runs; if it
   does not, the script says how to recover with that backup.

An older version refuses to start on a database a newer version has changed (it says which
version to run, or which `migrate down-to` to run with the newer one), rather than running on
tables it does not know.

## Configuration — `.env`

[`.env`](.env) holds the deployment's defaults. Docker Compose reads it on its own, and
`deploy.sh` reads it for every target. A variable already set in your shell wins over the
file, e.g. `HTTP_PORT=9090 ./deploy.sh --install --dockerdesktop`. It holds **no secrets**:
passwords and keys are files in `SECRETS_DIR` (Docker) or the `cactusinv-secrets` Secret
(k3s/k8s), generated by `--install`. A change takes effect at the next `./deploy.sh --install`
(it is safe to re-run; data is kept). With `--ssh-access`, the folders are on the remote host.

"Docker" below means `--dockerdesktop`, with or without `--ssh-access`.

### Folders (Docker)

On k3s/k8s these are not used: the data lives in volume claims (see
[k3s / k8s](#k3s--k8s)). Relative paths are relative to `.env`.

| Setting | Default | What it does |
|---|---|---|
| `DATA_DIR` | `/data/cactusinv` | Parent of the folders below. `deploy.sh` only names it in the `--uninstall` message; the four settings below are what is used, so move them together if you change it. |
| `MARIADB_DATA_DIR` | `…/mariadb` | MariaDB's database files. Keep it on a local disk, not a network share. |
| `INVOICES_DIR` | `…/invoices` | Exported invoice PDFs, by year. Settings → System Settings can map it to another host folder; a value set in the shell wins over that. |
| `BACKUP_DIR` | `…/backups` | Encrypted backups (scheduled, `--backup-now`, and uploaded ones). Can also be mapped in System Settings. Copy them off this machine. |
| `SECRETS_DIR` | `…/secrets` | Generated secret files: `db_password`, `db_root_password`, `app_key` (encrypts the SMTP password saved in the app), `backup_key` (encrypts backups; **keep a copy elsewhere**, a backup cannot be read without it), and `smtp_password` (empty; the fallback server's password, see E-mail). `--delete-uninstall` deletes it. |

### Network and image

| Setting | Default | What it does |
|---|---|---|
| `HTTP_PORT` | `8080` | Docker: host port for the web app and API (`http://localhost:8080`). |
| `MAILPIT_PORT` | `8025` | Docker: host port for the Mailpit test mailbox's web page (when `COMPOSE_PROFILES` includes `mailpit`). |
| `CACTUSINV_IMAGE` | `cactusinv:latest` | Name of the image `--install` builds and runs. On k3s/k8s the tag becomes the version (`cactusinv:1.14.0`); for `--k8s` it is pushed to `K8S_REGISTRY`. |

### Application (all targets)

| Setting | Default | What it does |
|---|---|---|
| `TZ` | `Africa/Johannesburg` | IANA time zone (e.g. `Europe/London`, `UTC`). Used for `BACKUP_TIMES`, log times, and the business time zone until one is saved in Settings → Business. An unknown name stops the app at start-up. |
| `CACTUSINV_LOG_LEVEL` | `info` | How much the app logs (JSON on stdout): `debug`, `info`, `warn` or `error`. Anything else means `info`. Logs never contain e-mail addresses, bank details or secrets. |
| `CACTUSINV_UPDATE_URL` | empty | Where Settings → System Settings → **Check for updates** reads the newest version: an `http(s)` URL of a file whose first line is a version like `1.14.0`. Empty = the `VERSION` file on GitHub's `main` branch (what `--upgrade` installs). `off` = never check. Only read when someone presses the button. |
| `CACTUSINV_COOKIE_SECURE` | `true` | `true` marks the sign-in cookie HTTPS-only. Keep it `true` behind HTTPS (a TLS proxy or ingress) and for `http://localhost`, which Chrome and Firefox treat as secure (if sign-in fails on
`localhost` in another browser, set `false`). Set `false` **only** if people open the app over plain `http://` on another address (e.g. `http://192.168.1.72:30080`), because otherwise the browser drops the cookie and sign-in fails. |
| `CACTUSINV_DEFAULT_ADMIN_USER` | `admin` | User name of the first Administrator. It is created only on the first start, when no user exists; changing it later does nothing. |
| `CACTUSINV_DEFAULT_ADMIN_PASSWORD` | `admin` | That user's first password. You are asked to change it at the first sign-in. |

### Database

| Setting | Default | What it does |
|---|---|---|
| `MARIADB_DATABASE` | `cactusinv` | Database name (1–57 letters, digits or `_`). A second database `<name>_restore` is used while restoring. Set it before the first install; changing it later starts an empty database. |
| `MARIADB_USER` | `cactusinv` | The app's database user (1–80 letters, digits or `_`). Its password is the `db_password` secret. Set it before the first install, like the name. |

### E-mail

Set the real mail server in the app: Settings → E-mail server (Gmail: `smtp.gmail.com`, port
587, STARTTLS, with a Google app password). These settings are only the **fallback** used
until a server is enabled there. By default that is the bundled Mailpit test mailbox, which
catches every message and sends nothing out.

| Setting | Default | What it does |
|---|---|---|
| `CACTUSINV_SMTP_HOST` | `mailpit` | Fallback SMTP server. Empty = no fallback (mail waits in the outbox until a server is set in the app). |
| `CACTUSINV_SMTP_PORT` | `1025` | Its port: usually 587 with `starttls`, 465 with `tls`, 25 or 1025 with `none`. |
| `CACTUSINV_SMTP_TLS` | `none` | `starttls` (upgrade to TLS after connecting), `tls` (TLS from the start) or `none` (plain; only for a local test server such as Mailpit). |
| `CACTUSINV_SMTP_USER` | empty | User name for the fallback server; empty = no sign-in. Its password goes in `${SECRETS_DIR}/smtp_password` (k3s/k8s: the `smtp_password` key of the `cactusinv-secrets` Secret), never in `.env`. |
| `CACTUSINV_SMTP_FROM` | `"PrimeSoft Accountsv <accounts@primesoft.co.za>"` | Sender of mail sent through the fallback server: `Name <address>`, quoted because of the spaces. |
| `COMPOSE_PROFILES` | `mailpit` | Docker only: optional containers to run. `mailpit` starts the test mailbox; empty runs without it (then point `CACTUSINV_SMTP_HOST` at a real server or leave it empty). On k3s/k8s Mailpit is always deployed. |

### Backups

| Setting | Default | What it does |
|---|---|---|
| `BACKUP_TIMES` | `02:00` | Daily times, in `TZ`, when the app takes an encrypted backup: 24-hour `HH:MM`, comma separated (`02:00,14:00`). |
| `BACKUP_RETENTION_DAYS` | `14` | Scheduled backups older than this are deleted; `0` keeps them all. |
| `BACKUP_MAX_UPLOAD_MB` | `256` | Largest backup file that can be uploaded in the app for a restore, in MiB (1–16384). |

### k3s / k8s

Used only with `--k3s` and `--k8s`.

| Setting | Default | What it does |
|---|---|---|
| `K8S_NAMESPACE` | `cactusinv` | Namespace everything is installed in (created if missing). |
| `K8S_NODE_PORT` | `30080` | NodePort the web app answers on (`http://<node>:30080`). It must be free and in the cluster's NodePort range (normally 30000–32767). |
| `K8S_MAILPIT_NODE_PORT` | `30025` | NodePort for the Mailpit test mailbox's web page. Same rules. |
| `K8S_STORAGE_CLASS` | empty | StorageClass for the three volume claims (database, invoices, backups). See below. |
| `K8S_VOLUME_DIR` | empty | k3s only: a folder on the node to keep the three volumes in (`mariadb/`, `invoices/`, `backups/`), e.g. `/data/k3s/local.volumes/cactusinv`. `deploy.sh` creates the folders, the class and the volumes. See below. |
| `K8S_MARIADB_STORAGE` | `10Gi` | Size of the database claim (`Gi` = GiB, `Mi` = MiB). |
| `K8S_INVOICES_STORAGE` | `5Gi` | Size of the invoices claim. |
| `K8S_BACKUP_STORAGE` | `10Gi` | Size of the backups claim. Allow room for `BACKUP_RETENTION_DAYS` × backups per day. |
| `K8S_REGISTRY` | empty | `--k8s` only: registry the image is pushed to (e.g. `registry.example.com/team`); you must be logged in to it (`docker login`). Not used by k3s, which imports the image straight into the node. |

The sizes apply when a claim is created. A claim that exists keeps its size: claims cannot
shrink, and `deploy.sh` does not grow them. To grow one, edit the claim with `kubectl` if its
StorageClass allows expansion.

#### `K8S_STORAGE_CLASS`: which values work

The value is the **name of a StorageClass that exists in your cluster**; there is no fixed
list. To see the names:

```
kubectl get storageclass                                   # k8s
ssh root@<node> k3s kubectl get storageclass               # k3s
```

- **Empty (the default):** the claims use the class marked `(default)`. That is `local-path`
  on k3s, `hostpath` on Docker Desktop's Kubernetes, and the cloud's disk class on a managed
  cluster (e.g. `gp2`/`gp3` on EKS, `standard-rwo` on GKE, `default`/`managed-csi` on AKS).
  It is the right choice unless you have a reason to pick another.
- **A class with a provisioner** (e.g. `local-path`, `longhorn`, `nfs-client`, `ceph-rbd`,
  `gp3`): the volume is created for you. The claims are `ReadWriteOnce`, which every class
  supports. For the database, prefer block or local storage over NFS.
- **A class with `kubernetes.io/no-provisioner`** (e.g. a `local-storage` class): nothing
  creates the volume. You must create a matching PersistentVolume for each claim first, or the
  claims stay `Pending` and the install stops after five minutes with "volume" in its message.

Choose it **before the first install**. A claim's StorageClass cannot change once it exists:
setting another class later makes `kubectl apply` fail with "spec is immutable". To move to
another class: take a backup (`--backup-now`), copy it and the backup key off the cluster,
`--delete-uninstall`, set the new class, `--install`, then `--restore` the backup.

On k3s, `local-path` stores the data in `/var/lib/rancher/k3s/storage/` on the node. Its volumes
are deleted with their claims (`--delete-uninstall`), so keep the backups elsewhere as well.

#### Keeping the volumes in a folder you choose (k3s)

`local-path` puts each volume in a generated folder (`<its path>/pvc-<id>_cactusinv_<claim>`).
To keep the data in a folder you name, set it in `.env`:

```
K8S_VOLUME_DIR=/data/k3s/local.volumes/cactusinv
K8S_STORAGE_CLASS=          # empty = local-storage
```

On `--install --k3s`, `deploy.sh` then:
- creates the storage class `K8S_STORAGE_CLASS` (default `local-storage`) if the cluster lacks
  it, as a `kubernetes.io/no-provisioner` class with `Retain`. A class of that name that does
  create its own volumes is refused;
- creates `mariadb/`, `invoices/` and `backups/` in the folder on the node, owned by the users
  the pods run as (MariaDB 999, the app 65532), mode 700;
- creates one PersistentVolume per claim (`cactusinv-mariadb-data`, …) in those folders, tied
  to its claim with `claimRef`, so the database cannot land in the backups folder. Each volume
  is sized as its claim (`K8S_*_STORAGE`); for a folder that is only a label, and the disk's
  free space is the real limit.

It needs a single-node k3s, because the folders are on one node. `--uninstall` keeps the
folders. `--delete-uninstall` deletes the volumes and the three folders with everything else.

Without `K8S_VOLUME_DIR`, a `K8S_STORAGE_CLASS` that does not exist stops the install at once
and lists the classes the cluster has. Before this check, the claims stayed `Pending` and the
install failed after five minutes with "unbound immediate PersistentVolumeClaims".

**Moving an existing install:** an install's claims keep the class they were created with
(`deploy.sh` refuses a different one). `--delete-uninstall` deletes the namespace, and with it the claims, their data,
the stored backups and the backup key. So take the backup **off the cluster** first:
1. Settings → Backup & restore → **Download a backup**, with a passphrase you keep. The file
   is saved on your computer.
2. `./deploy.sh --delete-uninstall --k3s --ssh-access root@<node>`.
3. Set `K8S_VOLUME_DIR` (and leave `K8S_STORAGE_CLASS` empty).
4. `./deploy.sh --install --k3s --ssh-access root@<node>`, sign in as `admin` / `admin`, then go to
   Settings → Backup & restore → **Restore from file** with the downloaded file and its
   passphrase.
5. Copy the new install's backup key off the cluster (see [Deploying](#deploying--deploysh)).

### Set by `deploy.sh`, not in `.env`

`deploy.sh` sets `CACTUSINV_VERSION`, `CACTUSINV_BUILD`, `CACTUSINV_DEPLOY_TARGET` and the
`CACTUSINV_HOST_*` folders itself; you don't set them. The app also reads lower-level
variables that the compose file and the manifest already set. For example, `CACTUSINV_SECRETS=vault`
with `CACTUSINV_VAULT_*` reads the secrets from HashiCorp Vault instead of files. All of them are listed in
[docs/ARCHITECTURE.md §6](docs/ARCHITECTURE.md#6-configuration--secrets).

## Users

Settings → Users & groups (on a phone: More → Users & groups; Administrators only) lists
everyone who can sign in, with their groups, status and last sign-in. What a user may do
comes from their **groups** (ADR-012):

- **Groups tab** — create a group, tick what it may do (each action on its own, per area:
  Dashboard; Invoices — view, create, edit, issue, e-mail, mark paid/overdue/void, delete;
  Customers; Statements — view, send; Schedules — incl. pause and resume separately; Products;
  Outbox — view, cancel, retry; Audit log — view, verify; and per Settings page: Business (view,
  edit), E-mail server (view, edit, test), Currencies (view, add, edit), Bank accounts (view, add,
  edit, delete), Import/Export (export and import separately) and System Settings (view, edit)) and who is in it; change or delete it
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
make build       # web checks + bundle, then bin/cactusinv with the web app embedded
make image       # the container image cactusinv:latest (deploy.sh builds it too)
pnpm -C web dev  # web app with hot reload, proxying /api to localhost:8080
make remote-test-int                      # integration tests on a Docker host over ssh
make remote-test-int REMOTE=user@host     # REMOTE is required; runs in /data/cactusinv/src there
```

`make remote-test-int` is for machines without Docker: it copies the working tree, including
uncommitted changes, to a fresh `REMOTE_DIR` (below `/data/cactusinv/`) and runs the MariaDB
integration tests there in the pinned `golang:1.26.10` image with the host's Docker socket.

Read [PLAN.md](PLAN.md), [docs/ENGINEERING_PRINCIPLES.md](docs/ENGINEERING_PRINCIPLES.md),
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) and [docs/adr](docs/adr) before changing code.

### Layout

```
cmd/cactusinv        composition root (serve, migrate, admin, system, backup, healthcheck, version)
internal/domain      money, currencies, invoices, terms, numbering, schedules, outbox — pure Go
internal/app         use cases
internal/ports       interfaces (+ in-memory fakes)
internal/adapters    mariadb (sqlc), http (OpenAPI), pdf, smtp, archive, secrets, worker
migrations           goose SQL migrations (embedded)
api/openapi          the API contract
web                  React + TypeScript + Vite + Bootstrap 5.3 + Bootstrap Icons (embedded in the binary)
deploy, deploy.sh    Docker Compose, k3s/k8s manifests, MariaDB init (restore staging database)
```

## Operations

- Health: `GET /healthz`, readiness: `GET /readyz`, Prometheus metrics: `GET /metrics`.
- Logs are JSON on stdout and never contain e-mail addresses or bank details.
- CLI inside the container: `/cactusinv migrate up|down`, `/cactusinv admin create <login>`
  and `/cactusinv admin set-password <login>` (password on stdin).
