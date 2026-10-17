package httpapi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ---- version ----

func (h *handlers) GetVersion(_ context.Context, _ GetVersionRequestObject) (GetVersionResponseObject, error) {
	return GetVersion200JSONResponse{Version: h.version, Build: h.build}, nil
}

// ---- e-mail server ----

func toAPIEmailSettings(v app.SMTPView) EmailSettings {
	s := v.Settings
	return EmailSettings{
		Enabled: s.Enabled, Host: s.Host, Port: s.Port, Username: s.Username, From: s.From,
		Tls: EmailSettingsTls(s.TLS), PasswordSet: v.PasswordSet, Version: s.Version,
		Warnings: v.Warnings,
	}
}

func (h *handlers) GetEmailSettings(ctx context.Context, _ GetEmailSettingsRequestObject) (GetEmailSettingsResponseObject, error) {
	v, err := h.svc.GetSMTPSettings(ctx)
	if err != nil {
		return nil, err
	}
	return GetEmailSettings200JSONResponse(toAPIEmailSettings(v)), nil
}

func (h *handlers) UpdateEmailSettings(ctx context.Context, req UpdateEmailSettingsRequestObject) (UpdateEmailSettingsResponseObject, error) {
	b := req.Body
	v, err := h.svc.UpdateSMTPSettings(ctx, domain.SMTPSettings{
		Enabled: b.Enabled, Host: b.Host, Port: b.Port, Username: b.Username, From: b.From, TLS: string(b.Tls),
		Version: b.Version,
	}, b.Password)
	if err != nil {
		return nil, err
	}
	return UpdateEmailSettings200JSONResponse(toAPIEmailSettings(v)), nil
}

func (h *handlers) SendTestEmail(ctx context.Context, req SendTestEmailRequestObject) (SendTestEmailResponseObject, error) {
	warnings, err := h.svc.SendTestEmail(ctx, req.Body.To)
	if err != nil {
		// Only the mail server's own answer (e.g. Gmail rejecting the password) is shown to
		// the user; a database or other internal error stays a 500 without details.
		var me *app.MailError
		if errors.As(err, &me) {
			return nil, &domain.ValidationError{Field: "smtp", Reason: "the mail server refused: " + rootCause(me.Err)}
		}
		return nil, err
	}
	return SendTestEmail200JSONResponse{Warnings: warnings}, nil
}

func rootCause(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		return msg[i+2:]
	}
	return msg
}

// ---- statements ----

func (h *handlers) GetStatement(ctx context.Context, req GetStatementRequestObject) (GetStatementResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.svc.GetStatement(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	c := st.Currency
	out := GetStatement200JSONResponse{
		CustomerId: st.Customer.ID, Currency: string(c), AsOf: toAPIDate(st.AsOf), Total: mu.format(st.Total, c),
		Lines: []StatementLine{},
	}
	out.Aging = toAPIAging(mu, st.Aging, c)
	for _, l := range st.Lines {
		out.Lines = append(out.Lines, StatementLine{
			InvoiceId: l.InvoiceID, Number: l.Number, IssueDate: toAPIDate(l.IssueDate), DueDate: toAPIDatePtr(l.DueDate),
			Amount: mu.format(l.Amount, c), DaysOverdue: l.DaysOverdue, Status: InvoiceStatus(l.Status),
		})
	}
	return out, nil
}

func (h *handlers) GetStatementPdf(ctx context.Context, req GetStatementPdfRequestObject) (GetStatementPdfResponseObject, error) {
	pdf, name, err := h.svc.StatementPDF(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetStatementPdf200ApplicationpdfResponse{
		Body: bytes.NewReader(pdf), ContentLength: int64(len(pdf)),
		Headers: GetStatementPdf200ResponseHeaders{ContentDisposition: `inline; filename="` + name + `"`},
	}, nil
}

func (h *handlers) SendStatement(ctx context.Context, req SendStatementRequestObject) (SendStatementResponseObject, error) {
	in := app.SendInput{IdempotencyKey: req.Params.IdempotencyKey}
	if b := req.Body; b != nil {
		in.To, in.CC, in.Subject, in.Body = b.To, b.Cc, b.Subject, b.Body
		if b.SendAt != nil {
			in.SendAt = b.SendAt.UTC()
		}
	}
	m, err := h.svc.SendStatement(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	return SendStatement202JSONResponse(toAPIEmail(*m)), nil
}

// ---- audit ----

func (h *handlers) ListAudit(ctx context.Context, req ListAuditRequestObject) (ListAuditResponseObject, error) {
	p := req.Params
	entries, total, err := h.svc.ListAudit(ctx, ports.AuditFilter{
		EntityType: p.EntityType, EntityID: p.EntityId, Actor: p.Actor, Action: p.Action, Page: page(p.Limit, p.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := ListAudit200JSONResponse{Items: make([]AuditEntry, len(entries)), Total: total}
	for i, e := range entries {
		out.Items[i] = AuditEntry{
			Seq: e.Seq, At: e.At.UTC(), ActorId: e.ActorID, Actor: e.ActorName, Action: e.Action,
			EntityType: e.EntityType, EntityId: e.EntityID, Summary: e.Summary, Hash: e.Hash,
		}
	}
	return out, nil
}

func (h *handlers) VerifyAudit(ctx context.Context, _ VerifyAuditRequestObject) (VerifyAuditResponseObject, error) {
	v, err := h.svc.VerifyAudit(ctx)
	if err != nil {
		return nil, err
	}
	return VerifyAudit200JSONResponse{Ok: v.OK, Entries: v.Entries, BrokenAt: v.BrokenAt, Reason: v.Reason}, nil
}

func (h *handlers) ListStatements(ctx context.Context, _ ListStatementsRequestObject) (ListStatementsResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.svc.ListStatements(ctx)
	if err != nil {
		return nil, err
	}
	out := ListStatements200JSONResponse{Items: []StatementSummary{}}
	for i, sum := range list {
		st := sum.Statement
		if i == 0 {
			out.AsOf = toAPIDate(st.AsOf)
		}
		c := st.Currency
		item := StatementSummary{
			CustomerId: st.Customer.ID, Code: st.Customer.Code, Name: st.Customer.Name, Email: st.Customer.Email,
			Currency: string(c), Total: mu.format(st.Total, c), OpenCount: len(st.Lines), Aging: toAPIAging(mu, st.Aging, c),
		}
		for _, l := range st.Lines {
			if l.DaysOverdue > 0 {
				item.OverdueCount++
			}
			item.MaxDaysOverdue = max(item.MaxDaysOverdue, l.DaysOverdue)
		}
		if m := sum.LastEmail; m != nil {
			item.LastEmail = &LastStatementEmail{CreatedAt: m.CreatedAt.UTC(), Status: string(m.Status)}
			if !m.SentAt.IsZero() {
				t := m.SentAt.UTC()
				item.LastEmail.SentAt = &t
			}
		}
		out.Items = append(out.Items, item)
	}
	if len(list) == 0 {
		today, err := h.svc.Today(ctx)
		if err != nil {
			return nil, err
		}
		out.AsOf = toAPIDate(today)
	}
	return out, nil
}

func toAPIAging(mu minorUnits, a domain.Aging, c domain.CurrencyCode) Aging {
	return Aging{
		Current: mu.format(a.Current, c), Days1to30: mu.format(a.Days1to30, c), Days31to60: mu.format(a.Days31to60, c),
		Days61to90: mu.format(a.Days61to90, c), Over90: mu.format(a.Over90, c),
	}
}

func (h *handlers) SendStatements(ctx context.Context, req SendStatementsRequestObject) (SendStatementsResponseObject, error) {
	res, err := h.svc.SendStatements(ctx, req.Body.CustomerIds, req.Body.IncludeZero)
	if err != nil {
		return nil, err
	}
	out := SendStatements202JSONResponse{Queued: res.Queued, Skipped: []StatementSkip{}}
	if out.Queued == nil {
		out.Queued = []string{}
	}
	for _, id := range req.Body.CustomerIds {
		if why, ok := res.Skipped[id]; ok {
			out.Skipped = append(out.Skipped, StatementSkip{CustomerId: id, Reason: why})
		}
	}
	return out, nil
}

// ---- system settings ----

func toAPISystemSettings(v app.SystemView) SystemSettings {
	s, d := v.Settings, v.Deployment
	return SystemSettings{
		SessionIdleMinutes: int(s.SessionIdle / time.Minute), SessionMaxHours: int((s.SessionMax + time.Hour - 1) / time.Hour),
		BackupHostDir: s.BackupHostDir, InvoicesHostDir: s.InvoicesHostDir, Version: s.Version,
		Deployment: DeploymentInfo{
			Target: d.Target, BackupDir: d.BackupDir, InvoicesDir: d.InvoicesDir, BackupHostDir: d.BackupHostDir, InvoicesHostDir: d.InvoicesHostDir,
		},
		PendingFolders: v.PendingFolders(),
	}
}

func (h *handlers) GetSystemSettings(ctx context.Context, _ GetSystemSettingsRequestObject) (GetSystemSettingsResponseObject, error) {
	v, err := h.svc.GetSystemSettings(ctx)
	if err != nil {
		return nil, err
	}
	return GetSystemSettings200JSONResponse(toAPISystemSettings(v)), nil
}

func (h *handlers) UpdateSystemSettings(ctx context.Context, req UpdateSystemSettingsRequestObject) (UpdateSystemSettingsResponseObject, error) {
	b := req.Body
	v, err := h.svc.UpdateSystemSettings(ctx, domain.SystemSettings{
		SessionIdle: time.Duration(b.SessionIdleMinutes) * time.Minute, SessionMax: time.Duration(b.SessionMaxHours) * time.Hour,
		BackupHostDir: b.BackupHostDir, InvoicesHostDir: b.InvoicesHostDir, Version: b.Version,
	})
	if err != nil {
		return nil, err
	}
	return UpdateSystemSettings200JSONResponse(toAPISystemSettings(v)), nil
}
