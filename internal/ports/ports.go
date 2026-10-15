// Package ports declares the interfaces the application layer needs from the outside
// world. Adapters implement them; use cases depend only on them.
package ports

import (
	"context"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

// Clock returns the current time. Inject a fake in tests.
type Clock interface {
	Now() time.Time
}

// IDGenerator returns new unique, time-ordered identifiers (UUIDv7).
type IDGenerator interface {
	NewID() string
}

// TxManager runs work inside a database transaction.
type TxManager interface {
	// WithinTx runs fn in one transaction. Repositories in r are bound to it. fn's error
	// rolls the transaction back.
	WithinTx(ctx context.Context, fn func(ctx context.Context, r Repos) error) error
	// Repos returns repositories that run outside an explicit transaction (reads).
	Repos() Repos
}

// Repos bundles every repository, bound to one transaction or to the pool.
type Repos struct {
	Business    BusinessRepository
	Currencies  CurrencyRepository
	Banks       BankAccountRepository
	Customers   CustomerRepository
	Products    ProductRepository
	Invoices    InvoiceRepository
	Sequences   SequenceRepository
	Schedules   ScheduleRepository
	Emails      EmailRepository
	Users       UserRepository
	Sessions    SessionRepository
	Idempotency IdempotencyRepository
	SMTP        SMTPRepository
	Audit       AuditRepository
}

// AuditFilter narrows the audit trail.
type AuditFilter struct {
	EntityType string
	EntityID   string
	Actor      string // substring of the actor name
	Action     string // prefix, e.g. "invoice."
	Page       Page
}

// AuditRepository is the append-only audit trail. There is deliberately no update or
// delete: the database rejects both.
type AuditRepository interface {
	// Append seals e onto the end of the hash chain (setting Seq, PrevHash, Hash). Call
	// inside the transaction of the change being audited.
	Append(ctx context.Context, e *domain.AuditEntry) error
	// List returns entries newest first, plus the total count.
	List(ctx context.Context, f AuditFilter) ([]domain.AuditEntry, int, error)
	// ForEntity returns one entity's entries, oldest first.
	ForEntity(ctx context.Context, entityType, entityID string) ([]domain.AuditEntry, error)
	// Chain returns up to limit entries with Seq > after, oldest first (for verification).
	Chain(ctx context.Context, after int64, limit int) ([]domain.AuditEntry, error)
}

// SMTPRepository stores the single SMTP settings row; the password is ciphertext.
type SMTPRepository interface {
	// Get returns the settings and encrypted password, or ErrNotFound before first save.
	Get(ctx context.Context) (domain.SMTPSettings, []byte, error)
	// Save inserts or updates; Version must match.
	Save(ctx context.Context, s domain.SMTPSettings, passwordEnc []byte) error
}

// Cipher encrypts small secrets at rest (AES-256-GCM).
type Cipher interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

// Page selects a slice of a list.
type Page struct {
	Limit  int
	Offset int
}

// Normalize applies defaults (50) and bounds (1..200).
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// BusinessRepository stores the single business profile row.
type BusinessRepository interface {
	// Get returns the profile, or ErrNotFound before it was ever saved.
	Get(ctx context.Context) (domain.BusinessProfile, error)
	// Save inserts or updates the profile; Version must match the stored version.
	Save(ctx context.Context, b domain.BusinessProfile) error
}

// CurrencyRepository stores currencies.
type CurrencyRepository interface {
	List(ctx context.Context) ([]domain.Currency, error)
	Get(ctx context.Context, code domain.CurrencyCode) (domain.Currency, error)
	Create(ctx context.Context, c domain.Currency) error
	Update(ctx context.Context, c domain.Currency) error
}

// BankAccountRepository stores bank accounts.
type BankAccountRepository interface {
	List(ctx context.Context, currency domain.CurrencyCode) ([]domain.BankAccount, error)
	Get(ctx context.Context, id string) (domain.BankAccount, error)
	Create(ctx context.Context, b domain.BankAccount) error
	Update(ctx context.Context, b domain.BankAccount) error
	Delete(ctx context.Context, id string) error
	// ClearDefault unsets is_default on every account of currency except keepID.
	ClearDefault(ctx context.Context, currency domain.CurrencyCode, keepID string) error
}

// CustomerFilter narrows a customer list.
type CustomerFilter struct {
	Query           string
	IncludeInactive bool
	Page            Page
}

// CustomerRepository stores customers.
type CustomerRepository interface {
	List(ctx context.Context, f CustomerFilter) ([]domain.Customer, int, error)
	Get(ctx context.Context, id string) (domain.Customer, error)
	Create(ctx context.Context, c domain.Customer) error
	Update(ctx context.Context, c domain.Customer) error
	Delete(ctx context.Context, id string) error
}

// ProductFilter narrows a product list (the look-up).
type ProductFilter struct {
	Query           string
	Currency        domain.CurrencyCode // only products priced in this currency
	IncludeInactive bool
	Page            Page
}

// ProductRepository stores products and their per-currency prices.
type ProductRepository interface {
	List(ctx context.Context, f ProductFilter) ([]domain.Product, int, error)
	Get(ctx context.Context, id string) (domain.Product, error)
	GetMany(ctx context.Context, ids []string) (map[string]domain.Product, error)
	Create(ctx context.Context, p domain.Product) error
	Update(ctx context.Context, p domain.Product) error
	Delete(ctx context.Context, id string) error
}

// InvoiceFilter narrows an invoice list.
type InvoiceFilter struct {
	Status     domain.InvoiceStatus
	CustomerID string
	Query      string
	Overdue    bool
	Today      domain.Date
	Page       Page
}

// CurrencyTotals summarise open invoices in one currency.
type CurrencyTotals struct {
	Currency     domain.CurrencyCode
	OpenCount    int
	Outstanding  int64
	OverdueCount int
	Overdue      int64
}

// InvoiceRepository stores invoices, lines and their audit events.
type InvoiceRepository interface {
	// List returns invoices without lines, newest first, plus the total count.
	List(ctx context.Context, f InvoiceFilter) ([]domain.Invoice, int, error)
	Get(ctx context.Context, id string) (*domain.Invoice, error)
	Create(ctx context.Context, inv *domain.Invoice) error
	// Update saves inv if inv.Version matches, then increments inv.Version.
	Update(ctx context.Context, inv *domain.Invoice) error
	Delete(ctx context.Context, id string) error
	Totals(ctx context.Context, today domain.Date) ([]CurrencyTotals, error)
	// CountForCustomer counts invoices of any status for a customer.
	CountForCustomer(ctx context.Context, customerID string) (int, error)
	// Monthly sums invoiced (by issue month) and paid (by payment month) amounts per
	// currency since the given date.
	Monthly(ctx context.Context, since domain.Date) (invoiced, paid []MonthTotal, err error)
	// TopDebtors returns the customers owing the most (per currency), largest first.
	TopDebtors(ctx context.Context, limit int) ([]Debtor, error)
	// ListUnexported returns issued invoices whose current version is not yet archived.
	ListUnexported(ctx context.Context, limit int) ([]ExportRef, error)
	// MarkExported records that version of the invoice was archived (no version bump).
	MarkExported(ctx context.Context, id string, version int) error
}

// MonthTotal is one currency's total for one month ("2026-10").
type MonthTotal struct {
	Currency domain.CurrencyCode
	Month    string
	Total    int64
	Count    int
}

// Debtor is a customer's outstanding balance in one currency.
type Debtor struct {
	CustomerID  string
	Name        string
	Currency    domain.CurrencyCode
	Outstanding int64
	OpenCount   int
}

// ExportRef identifies one invoice version to archive.
type ExportRef struct {
	ID      string
	Version int
}

// SequenceRepository allocates gap-free invoice numbers. Call inside a transaction.
type SequenceRepository interface {
	// Next locks the sequence, returns its next value and increments it.
	Next(ctx context.Context) (int64, error)
	// Peek returns the next value without allocating it.
	Peek(ctx context.Context) (int64, error)
	// Set changes the next value; it may never go below an already issued number.
	Set(ctx context.Context, next int64) error
}

// ScheduleFilter narrows a schedule list.
type ScheduleFilter struct {
	CustomerID string
	Status     domain.ScheduleStatus
	Page       Page
}

// ScheduleRepository stores recurring invoice schedules.
type ScheduleRepository interface {
	List(ctx context.Context, f ScheduleFilter) ([]domain.Schedule, int, error)
	Get(ctx context.Context, id string) (*domain.Schedule, error)
	Create(ctx context.Context, s *domain.Schedule) error
	Update(ctx context.Context, s *domain.Schedule) error
	Delete(ctx context.Context, id string) error
	// ClaimDue locks up to limit due, active schedules (FOR UPDATE SKIP LOCKED).
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*domain.Schedule, error)
	// InsertRun records a processed period; ErrConflict if it was already processed.
	InsertRun(ctx context.Context, run domain.ScheduleRun) error
	ListRuns(ctx context.Context, scheduleID string, limit int) ([]domain.ScheduleRun, error)
}

// EmailFilter narrows the outbox list.
type EmailFilter struct {
	Status     domain.EmailStatus
	InvoiceID  string
	CustomerID string // statement e-mails of a customer
	Page       Page
}

// OutboxStats describe the outbox for metrics.
type OutboxStats struct {
	Pending       int
	OldestPending time.Time
}

// EmailRepository stores the e-mail outbox.
type EmailRepository interface {
	// Create inserts m; ErrConflict when its idempotency key exists.
	Create(ctx context.Context, m *domain.EmailMessage) error
	Get(ctx context.Context, id string) (*domain.EmailMessage, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*domain.EmailMessage, error)
	Update(ctx context.Context, m *domain.EmailMessage) error
	List(ctx context.Context, f EmailFilter) ([]domain.EmailMessage, int, error)
	// ClaimDue locks up to limit claimable messages (FOR UPDATE SKIP LOCKED).
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]*domain.EmailMessage, error)
	Stats(ctx context.Context) (OutboxStats, error)
}

// UserRepository stores administrators.
type UserRepository interface {
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	Get(ctx context.Context, id string) (domain.User, error)
	Create(ctx context.Context, u domain.User) error
	UpdatePassword(ctx context.Context, id, hash string) error
	Count(ctx context.Context) (int, error)
}

// SessionRepository stores browser sessions by token hash.
type SessionRepository interface {
	Create(ctx context.Context, s domain.Session) error
	Get(ctx context.Context, tokenHash string) (domain.Session, error)
	Delete(ctx context.Context, tokenHash string) error
	DeleteExpired(ctx context.Context, now time.Time) error
}

// IdempotencyRecord is a stored response for an Idempotency-Key.
type IdempotencyRecord struct {
	Key         string
	Fingerprint string // method + path + body hash
	Status      int    // 0 while the first request is still in flight
	Body        []byte
	CreatedAt   time.Time
}

// IdempotencyRepository stores responses to replay for repeated Idempotency-Keys.
type IdempotencyRepository interface {
	// Reserve inserts an in-flight record; ErrConflict when the key exists.
	Reserve(ctx context.Context, rec IdempotencyRecord) error
	Get(ctx context.Context, key string) (IdempotencyRecord, error)
	Complete(ctx context.Context, key string, status int, body []byte) error
	Release(ctx context.Context, key string) error
	DeleteBefore(ctx context.Context, t time.Time) error
}

// Attachment is a file attached to an e-mail.
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Mail is one outgoing e-mail.
type Mail struct {
	MessageID   string
	To          []string
	CC          []string
	ReplyTo     string
	Subject     string
	Body        string
	Attachments []Attachment
}

// Mailer delivers e-mail.
type Mailer interface {
	Send(ctx context.Context, m Mail) error
}

// InvoiceDocument is everything printed on an invoice.
type InvoiceDocument struct {
	Invoice  *domain.Invoice
	Business domain.BusinessProfile
	Currency domain.Currency
	// Disclaimer is resolved each time the PDF is rendered (domain.Disclaimer).
	Disclaimer string
}

// StatementDocument is everything printed on a customer statement.
type StatementDocument struct {
	Statement  domain.Statement
	Business   domain.BusinessProfile
	Currency   domain.Currency
	Bank       domain.BankDetails // the customer's pay-into account (may be empty)
	Disclaimer string
}

// InvoiceRenderer renders invoices and statements as PDF.
type InvoiceRenderer interface {
	RenderPDF(ctx context.Context, doc InvoiceDocument) ([]byte, error)
	RenderStatement(ctx context.Context, doc StatementDocument) ([]byte, error)
}

// InvoiceArchive stores exported invoice PDFs (the invoices volume).
type InvoiceArchive interface {
	// Save writes data at the relative path, replacing any previous version atomically.
	Save(ctx context.Context, path string, data []byte) error
}

// PasswordHasher hashes and verifies passwords (argon2id).
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) (bool, error)
}

// Secrets reads secrets (HashiCorp Vault KV v2 in production).
type Secrets interface {
	Get(ctx context.Context, key string) (string, error)
}
