# Placeholders

cactusInv fills placeholders in three places. Each kind has its own brackets and only works
where it is listed here. In e-mails and line descriptions, a placeholder that is not known
there is left exactly as written, so a typo shows up in the result rather than disappearing;
an invoice number format with an unknown placeholder cannot be saved.

| Where | Brackets | Example |
|---|---|---|
| [E-mail subjects and bodies](#1-e-mail-subjects-and-bodies) | double braces | `Invoice {{invoice_number}} from {{business_name}}` |
| [Invoice number format](#2-invoice-number-format) | single braces | `{code}/{dd}{mm}-{seq}/{yy}` |
| [Recurring-invoice line descriptions](#3-recurring-invoice-line-descriptions) | single braces | `Support for {period}` |

## 1. E-mail subjects and bodies

Used in every e-mail text the app sends:

- **Settings → Business**: the default invoice e-mail subject and body.
- **Send** on an invoice, and the statement e-mail on **Statements**.
- **Schedules**: the subject and body of a recurring invoice or e-mail schedule. The editor
  lists the placeholders that work for the type you picked.

The values are filled when the e-mail is queued. Line breaks in a subject are turned into
spaces.

### Invoice e-mails

These cover sending an invoice, a recurring invoice's e-mail, and the "unsent" and
"outstanding" e-mail schedules.

| Placeholder | Filled with | Example |
|---|---|---|
| `{{invoice_number}}` | the invoice number (see [§2](#2-invoice-number-format)) | `CD/2108-1195/26` |
| `{{customer_name}}` | the billed customer's name, as printed on the invoice | `Acme Commercial Spares (Pty) Limited` |
| `{{attention}}` | the customer's "attention" contact on the invoice; empty if none | `Accounts Payable` |
| `{{total}}` | the invoice total: currency symbol (or code if it has no symbol), a space, then the amount with thousands separators and the currency's decimals | `R 3,251.16` |
| `{{due_date}}` | the due date | `7 September 2026` |
| `{{issue_date}}` | the issue date | `21 August 2026` |
| `{{date}}` | same as `{{issue_date}}` | `21 August 2026` |
| `{{business_name}}` | your business name (Settings → Business) | `Example Software Consulting` |

### Statement e-mails

These cover the statement e-mail on Statements and the "statement" e-mail schedule.

| Placeholder | Filled with | Example |
|---|---|---|
| `{{customer_name}}` | the customer's name | `Acme Commercial Spares (Pty) Limited` |
| `{{attention}}` | the customer's "attention" contact; empty if none | `Accounts Payable` |
| `{{total}}` | the balance outstanding, formatted as for invoices | `R 12,480.00` |
| `{{date}}` | the statement's as-at date | `17 October 2026` |
| `{{issue_date}}` | same as `{{date}}` | `17 October 2026` |
| `{{business_name}}` | your business name | `Example Software Consulting` |

A statement is about no single invoice, so `{{invoice_number}}` and `{{due_date}}` are empty
in a statement e-mail.

### Example

```
Subject: Invoice {{invoice_number}} from {{business_name}}

Dear {{customer_name}},

Please find attached invoice {{invoice_number}} dated {{date}} for {{total}}, due {{due_date}}.

Kind regards,
{{business_name}}
```

becomes

```
Subject: Invoice CD/2108-1195/26 from Example Software Consulting

Dear Acme Commercial Spares (Pty) Limited,

Please find attached invoice CD/2108-1195/26 dated 21 August 2026 for R 3,251.16, due 7 September 2026.

Kind regards,
Example Software Consulting
```

## 2. Invoice number format

Set in **Settings → Business → Invoice number format**. A number is given when an invoice is
**issued**; a draft has none. The format must contain `{seq}`. Apart from the placeholders
below it may contain any literal text (`/`, `-`, letters), but no other `{` or `}`. With the
longest customer code, a number can be at most 64 characters.

| Placeholder | Filled with | Example |
|---|---|---|
| `{seq}` | **required.** The next value of the gap-free invoice sequence: one sequence shared by all customers, with no padding, starting at the "next number" in Settings → Business. Every issued invoice uses the next value; numbers are never skipped or reused (a restore never rewinds them). | `1195` |
| `{code}` | the customer's invoice code (Customers → code: 1 to 10 capital letters or digits) | `CD` |
| `{dd}` | the issue day, 2 digits | `21` |
| `{mm}` | the issue month, 2 digits | `08` |
| `{yy}` | the issue year, 2 digits | `26` |
| `{yyyy}` | the issue year, 4 digits | `2026` |

Examples for customer `CD`, sequence 1195, issued 21 August 2026:

| Format | Number |
|---|---|
| `{code}/{dd}{mm}-{seq}/{yy}` (the default for new installs) | `CD/2108-1195/26` |
| `{code}/{seq}/{yy}` | `CD/1195/26` |
| `INV-{yyyy}-{seq}` | `INV-2026-1195` |
| `{yyyy}{mm}{dd}-{code}-{seq}` | `20260821-CD-1195` |

Changing the format affects invoices issued from then on; issued invoices keep their numbers.

## 3. Recurring-invoice line descriptions

Used in the **line descriptions of a recurring-invoice schedule**: the product's own line
description, or the text you typed for that line. They name the billing period an invoice
was raised for.

| Placeholder | Filled with | Example |
|---|---|---|
| `{month}` | the month of the scheduled date | `November` |
| `{year}` | the year of the scheduled date | `2026` |
| `{period}` | month and year | `November 2026` |

The value comes from the date the run was **scheduled for**, not the day it ran. If the
weekend rule moves a run on Sunday 1 November to Friday 30 October, `{month}` is still
`November`. A run caught up late (for example after downtime) keeps its own month too. For
quarterly and yearly schedules the month is the scheduled date's month (for example
`February` for a quarter that starts in February).

Example: a product line description `Stock system usage for {period}` on the November run
becomes `Stock system usage for November 2026`.

These placeholders work only in line descriptions, not in e-mail texts or notes.
