package mariadb

import (
	"context"
	"database/sql"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/mariadb/sqlcgen"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type emailRepo struct{ q *sqlcgen.Queries }

func toEmail(r sqlcgen.EmailOutbox) *domain.EmailMessage {
	return &domain.EmailMessage{
		ID: r.ID, Kind: domain.EmailKind(r.Kind), InvoiceID: r.InvoiceID.String, CustomerID: r.CustomerID.String, To: splitList(r.ToAddrs), CC: splitList(r.CcAddrs), Subject: r.Subject,
		Body: r.Body, Status: domain.EmailStatus(r.Status), Attempts: int(r.Attempts), SendAt: r.SendAt.UTC(),
		NextAttemptAt: r.NextAttemptAt.UTC(), LastError: r.LastError, SentAt: fromNullTime(r.SentAt),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(), IdempotencyKey: r.IdempotencyKey.String,
	}
}

func (r emailRepo) Create(ctx context.Context, m *domain.EmailMessage) error {
	return mapErr(r.q.InsertEmail(ctx, sqlcgen.InsertEmailParams{
		ID: m.ID, Kind: string(emailKind(m.Kind)), InvoiceID: nullString(m.InvoiceID), CustomerID: nullString(m.CustomerID), ToAddrs: joinList(m.To), CcAddrs: joinList(m.CC), Subject: m.Subject,
		Body: m.Body, Status: string(m.Status), Attempts: i32(m.Attempts), SendAt: m.SendAt.UTC(),
		NextAttemptAt: m.NextAttemptAt.UTC(), LastError: m.LastError, SentAt: nullTime(m.SentAt),
		IdempotencyKey: nullString(m.IdempotencyKey), CreatedAt: m.CreatedAt.UTC(), UpdatedAt: m.UpdatedAt.UTC(),
	}))
}

func (r emailRepo) Lock(ctx context.Context, id string) (*domain.EmailMessage, error) {
	row, err := r.q.LockEmail(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return toEmail(row), nil
}

func (r emailRepo) Get(ctx context.Context, id string) (*domain.EmailMessage, error) {
	row, err := r.q.GetEmail(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return toEmail(row), nil
}

func (r emailRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.EmailMessage, error) {
	row, err := r.q.GetEmailByIdempotencyKey(ctx, nullString(key))
	if err != nil {
		return nil, mapErr(err)
	}
	return toEmail(row), nil
}

func (r emailRepo) Update(ctx context.Context, m *domain.EmailMessage) error {
	n, err := r.q.UpdateEmail(ctx, sqlcgen.UpdateEmailParams{
		ToAddrs: joinList(m.To), CcAddrs: joinList(m.CC), Subject: m.Subject, Body: m.Body, Status: string(m.Status),
		Attempts: i32(m.Attempts), NextAttemptAt: m.NextAttemptAt.UTC(), LastError: m.LastError,
		SentAt: nullTime(m.SentAt), UpdatedAt: m.UpdatedAt.UTC(), ID: m.ID,
	})
	return affected(n, err, domain.ErrNotFound)
}

func (r emailRepo) List(ctx context.Context, f ports.EmailFilter) ([]domain.EmailMessage, int, error) {
	p := f.Page.Normalize()
	rows, err := r.q.ListEmails(ctx, sqlcgen.ListEmailsParams{
		Status: string(f.Status), InvoiceID: filter(f.InvoiceID), CustomerID: filter(f.CustomerID),
		Limit: i32(p.Limit), Offset: i32(p.Offset),
	})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	total, err := r.q.CountEmails(ctx, sqlcgen.CountEmailsParams{Status: string(f.Status), InvoiceID: filter(f.InvoiceID), CustomerID: filter(f.CustomerID)})
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.EmailMessage, len(rows))
	for i, row := range rows {
		out[i] = *toEmail(row)
	}
	return out, int(total), nil
}

func (r emailRepo) ClaimDue(ctx context.Context, now time.Time, limit int) ([]*domain.EmailMessage, error) {
	if held, err := r.q.AutomationHeldShared(ctx); err != nil || held {
		return nil, mapErr(err)
	}
	rows, err := r.q.ClaimDueEmails(ctx, sqlcgen.ClaimDueEmailsParams{
		Now: now.UTC(), StaleBefore: now.Add(-domain.StaleSendingAfter).UTC(), Limit: i32(limit),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]*domain.EmailMessage, len(rows))
	for i, row := range rows {
		out[i] = toEmail(row)
	}
	return out, nil
}

func (r emailRepo) CountSending(ctx context.Context, now time.Time) (int, error) {
	n, err := r.q.CountSendingEmails(ctx, now.Add(-domain.StaleSendingAfter).UTC())
	return int(n), mapErr(err)
}

func (r emailRepo) Stats(ctx context.Context) (ports.OutboxStats, error) {
	row, err := r.q.OutboxStats(ctx)
	if err != nil {
		return ports.OutboxStats{}, mapErr(err)
	}
	st := ports.OutboxStats{Pending: int(row.Pending)}
	if t, ok := row.Oldest.(time.Time); ok {
		st.OldestPending = t.UTC()
	}
	return st, nil
}

// filter passes an optional filter value; "" means "any" (it must not be NULL, which would
// make the query's `? = ”` test unknown).
func filter(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func emailKind(k domain.EmailKind) domain.EmailKind {
	if k == "" {
		return domain.EmailKindInvoice
	}
	return k
}

type userRepo struct{ q *sqlcgen.Queries }

func toUser(u sqlcgen.User) domain.User {
	return domain.User{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, CreatedAt: u.CreatedAt.UTC(),
		DisabledAt: fromNullTime(u.DisabledAt), MustChangePassword: u.MustChangePassword, LastLoginAt: fromNullTime(u.LastLoginAt),
	}
}

func (r userRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	u, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		return domain.User{}, mapErr(err)
	}
	return toUser(u), nil
}

func (r userRepo) Get(ctx context.Context, id string) (domain.User, error) {
	u, err := r.q.GetUser(ctx, id)
	if err != nil {
		return domain.User{}, mapErr(err)
	}
	return toUser(u), nil
}

func (r userRepo) Lock(ctx context.Context, id string) (domain.User, error) {
	u, err := r.q.LockUser(ctx, id)
	if err != nil {
		return domain.User{}, mapErr(err)
	}
	return toUser(u), nil
}

func (r userRepo) List(ctx context.Context) ([]domain.User, error) {
	rows, err := r.q.ListUsers(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.User, len(rows))
	for i, u := range rows {
		out[i] = toUser(u)
	}
	return out, nil
}

func (r userRepo) Create(ctx context.Context, u domain.User) error {
	return mapErr(r.q.InsertUser(ctx, sqlcgen.InsertUserParams{
		ID: u.ID, Email: u.Email, PasswordHash: u.PasswordHash, CreatedAt: u.CreatedAt.UTC(), MustChangePassword: u.MustChangePassword,
	}))
}

func (r userRepo) UpdatePassword(ctx context.Context, id, hash string, mustChange bool) error {
	n, err := r.q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{PasswordHash: hash, MustChangePassword: mustChange, ID: id})
	return affected(n, err, domain.ErrNotFound)
}

func (r userRepo) SetDisabled(ctx context.Context, id string, at time.Time) error {
	n, err := r.q.SetUserDisabled(ctx, sqlcgen.SetUserDisabledParams{DisabledAt: nullTime(at), ID: id})
	return affected(n, err, domain.ErrNotFound)
}

func (r userRepo) TouchLogin(ctx context.Context, id string, at time.Time) error {
	n, err := r.q.TouchUserLogin(ctx, sqlcgen.TouchUserLoginParams{LastLoginAt: nullTime(at), ID: id})
	return affected(n, err, domain.ErrNotFound)
}

func (r userRepo) LockActive(ctx context.Context) ([]string, error) {
	ids, err := r.q.LockActiveUsers(ctx)
	return ids, mapErr(err)
}

func (r userRepo) Count(ctx context.Context) (int, error) {
	n, err := r.q.CountUsers(ctx)
	return int(n), mapErr(err)
}

type sessionRepo struct{ q *sqlcgen.Queries }

func (r sessionRepo) Create(ctx context.Context, s domain.Session) error {
	return mapErr(r.q.InsertSession(ctx, sqlcgen.InsertSessionParams{
		TokenHash: s.TokenHash, UserID: s.UserID, ExpiresAt: s.ExpiresAt.UTC(), CreatedAt: s.CreatedAt.UTC(),
	}))
}

func (r sessionRepo) Get(ctx context.Context, tokenHash string) (domain.Session, error) {
	s, err := r.q.GetSession(ctx, tokenHash)
	if err != nil {
		return domain.Session{}, mapErr(err)
	}
	return domain.Session{TokenHash: s.TokenHash, UserID: s.UserID, ExpiresAt: s.ExpiresAt.UTC(), CreatedAt: s.CreatedAt.UTC()}, nil
}

func (r sessionRepo) Delete(ctx context.Context, tokenHash string) error {
	return mapErr(r.q.DeleteSession(ctx, tokenHash))
}

func (r sessionRepo) DeleteAll(ctx context.Context) error {
	return mapErr(r.q.DeleteAllSessions(ctx))
}

func (r sessionRepo) DeleteForUser(ctx context.Context, userID, exceptHash string) error {
	return mapErr(r.q.DeleteUserSessions(ctx, sqlcgen.DeleteUserSessionsParams{UserID: userID, TokenHash: exceptHash}))
}

func (r sessionRepo) DeleteExpired(ctx context.Context, now time.Time) error {
	return mapErr(r.q.DeleteExpiredSessions(ctx, now.UTC()))
}

type idempotencyRepo struct{ q *sqlcgen.Queries }

func (r idempotencyRepo) Reserve(ctx context.Context, rec ports.IdempotencyRecord) error {
	return mapErr(r.q.InsertIdempotencyKey(ctx, sqlcgen.InsertIdempotencyKeyParams{
		IdemKey: rec.Key, Fingerprint: rec.Fingerprint, Status: i32(rec.Status), Body: append([]byte{}, rec.Body...),
		CreatedAt: rec.CreatedAt.UTC(),
	}))
}

func (r idempotencyRepo) Get(ctx context.Context, key string) (ports.IdempotencyRecord, error) {
	k, err := r.q.GetIdempotencyKey(ctx, key)
	if err != nil {
		return ports.IdempotencyRecord{}, mapErr(err)
	}
	return ports.IdempotencyRecord{Key: k.IdemKey, Fingerprint: k.Fingerprint, Status: int(k.Status), Body: k.Body, CreatedAt: k.CreatedAt.UTC()}, nil
}

func (r idempotencyRepo) Complete(ctx context.Context, key string, status int, body []byte) error {
	n, err := r.q.CompleteIdempotencyKey(ctx, sqlcgen.CompleteIdempotencyKeyParams{Status: i32(status), Body: body, IdemKey: key})
	return affected(n, err, domain.ErrNotFound)
}

func (r idempotencyRepo) Release(ctx context.Context, key string) error {
	return mapErr(r.q.DeleteIdempotencyKey(ctx, key))
}

func (r idempotencyRepo) DeleteAll(ctx context.Context) error {
	return mapErr(r.q.DeleteAllIdempotencyKeys(ctx))
}

func (r idempotencyRepo) DeleteBefore(ctx context.Context, t time.Time) error {
	return mapErr(r.q.DeleteIdempotencyKeysBefore(ctx, t.UTC()))
}
