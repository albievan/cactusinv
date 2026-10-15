package httpapi

import (
	"bytes"
	"context"
	"strings"

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
	if err := h.svc.SendTestEmail(ctx, req.Body.To); err != nil {
		if strings.Contains(err.Error(), "validation") || isDomain(err) {
			return nil, err
		}
		// Surface the mail server's answer (e.g. Gmail rejecting the password) to the user.
		return nil, &domain.ValidationError{Field: "smtp", Reason: "the mail server refused: " + rootCause(err)}
	}
	return SendTestEmail204Response{}, nil
}

func isDomain(err error) bool { return problemFor(err).Status < 500 }

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
	out.Aging.Current = mu.format(st.Aging.Current, c)
	out.Aging.Days1to30 = mu.format(st.Aging.Days1to30, c)
	out.Aging.Days31to60 = mu.format(st.Aging.Days31to60, c)
	out.Aging.Days61to90 = mu.format(st.Aging.Days61to90, c)
	out.Aging.Over90 = mu.format(st.Aging.Over90, c)
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
