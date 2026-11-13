package httpapi

import (
	"bytes"
	"context"
	"errors"
	"strings"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// ---- quotes (M34) ----

func toAPIQuote(q *domain.Quote, mu minorUnits, today domain.Date) Quote {
	c := q.Currency
	lines := make([]Line, len(q.Lines))
	for i, l := range q.Lines {
		lines[i] = Line{
			ProductId: l.ProductID, Description: l.Description, Quantity: l.Quantity.String(),
			UnitPrice: mu.format(l.UnitPrice, c), DiscountPercent: l.Discount.String(), Amount: mu.format(l.Amount(), c),
		}
	}
	return Quote{
		Id: q.ID, Number: q.Number, Status: QuoteStatus(q.Status), Stage: QuoteStage(q.Stage(today)),
		CustomerId: q.CustomerID, AdhocCustomerId: q.AdhocCustomerID, Currency: string(c),
		IssueDate: toAPIDate(q.IssueDate), ValidUntil: toAPIDate(q.ValidUntil), SalesPerson: q.SalesPerson,
		Reference: q.Reference, BillTo: toAPIParty(q.BillTo), Notes: q.Notes, Terms: q.Terms,
		VatPercent: q.TaxRate.String(), Lines: lines,
		Totals: Totals{
			Subtotal: mu.format(q.Totals.Subtotal, c), Discount: mu.format(q.Totals.Discount, c),
			Vat: mu.format(q.Totals.Tax, c), Total: mu.format(q.Totals.Total, c),
		},
		Fingerprint: q.Fingerprint(), InvoiceId: q.InvoiceID, SentAt: timePtr(q.SentAt), AcceptedAt: timePtr(q.AcceptedAt),
		DeclinedAt: timePtr(q.DeclinedAt), DeclineReason: q.DeclineReason, ConvertedAt: timePtr(q.ConvertedAt),
		AnsweredBy: QuoteAnsweredBy(q.AnsweredBy), RenewedFrom: q.RenewedFrom,
		CreatedAt: q.CreatedAt.UTC(), UpdatedAt: q.UpdatedAt.UTC(), Version: q.Version,
	}
}

// quoteInput parses a QuoteInput; amounts are in currency.
func quoteInput(b QuoteInput, currency domain.CurrencyCode, mu minorUnits) (app.QuoteInput, error) {
	tax, err := optionalPercent("vatPercent", b.VatPercent)
	if err != nil {
		return app.QuoteInput{}, err
	}
	in := app.QuoteInput{
		CustomerID: b.CustomerId, AdhocCustomerID: b.AdhocCustomerId, IssueDate: fromAPIDatePtr(b.IssueDate),
		ValidUntil: fromAPIDatePtr(b.ValidUntil), SalesPerson: b.SalesPerson, Reference: b.Reference,
		BillTo: fromAPIPartyPtr(b.BillTo), Notes: b.Notes, Terms: b.Terms, TaxRate: tax,
	}
	in.Lines, err = lineInputs(b.Lines, currency, mu)
	return in, err
}

func (h *handlers) ListQuotes(ctx context.Context, req ListQuotesRequestObject) (ListQuotesResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	qs, total, err := h.svc.ListQuotes(ctx, ports.QuoteFilter{
		Status: domain.QuoteStatus(p.Status), Expired: p.Expired, CustomerID: p.CustomerId, AdhocCustomerID: p.AdhocCustomerId,
		AnsweredBy: domain.QuoteAnswerer(p.AnsweredBy), Query: p.Q, Page: page(p.Limit, p.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Quote, len(qs))
	for i := range qs {
		out[i] = toAPIQuote(&qs[i], mu, today)
	}
	return ListQuotes200JSONResponse{Items: out, Total: total}, nil
}

// newQuoteCurrency is the currency a new quote is priced in: its customer's or ad-hoc
// customer's (the app checks the rest).
func (h *handlers) newQuoteCurrency(ctx context.Context, b QuoteInput) (domain.CurrencyCode, error) {
	switch {
	case b.CustomerId != "" && b.AdhocCustomerId == "":
		c, err := h.svc.GetCustomer(ctx, b.CustomerId)
		if errors.Is(err, domain.ErrNotFound) {
			return "", &domain.ValidationError{Field: "customerId", Reason: "unknown customer"}
		}
		return c.Currency, err
	case b.AdhocCustomerId != "" && b.CustomerId == "":
		a, err := h.svc.GetAdhocCustomer(ctx, b.AdhocCustomerId)
		if errors.Is(err, domain.ErrNotFound) {
			return "", &domain.ValidationError{Field: "adhocCustomerId", Reason: "unknown ad-hoc customer"}
		}
		return a.Currency, err
	default:
		return "", &domain.ValidationError{Field: "customerId", Reason: "choose a customer or an ad-hoc customer"}
	}
}

func (h *handlers) CreateQuote(ctx context.Context, req CreateQuoteRequestObject) (CreateQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := h.newQuoteCurrency(ctx, *req.Body)
	if err != nil {
		return nil, err
	}
	in, err := quoteInput(*req.Body, cur, mu)
	if err != nil {
		return nil, err
	}
	q, err := h.svc.CreateQuote(ctx, in)
	if err != nil {
		return nil, err
	}
	return CreateQuote201JSONResponse(toAPIQuote(q, mu, today)), nil
}

func toAPIAdhocCustomer(a domain.AdhocCustomer) AdhocCustomer {
	return AdhocCustomer{
		Id: a.ID, Name: a.Name, Email: a.Email, Attention: a.Attention, Address: a.Address, Phone: a.Phone,
		TaxNumber: a.TaxNumber, Currency: string(a.Currency), CustomerId: a.CustomerID, ConvertedAt: timePtr(a.ConvertedAt),
		CreatedAt: timePtr(a.CreatedAt), Version: a.Version,
	}
}

func fromAPIAdhocCustomer(id string, a AdhocCustomer) domain.AdhocCustomer {
	return domain.AdhocCustomer{
		ID: id, Name: a.Name, Email: a.Email, Attention: a.Attention, Address: a.Address, Phone: a.Phone,
		TaxNumber: a.TaxNumber, Currency: domain.CurrencyCode(strings.ToUpper(a.Currency)), Version: a.Version,
	}
}

func (h *handlers) GetQuote(ctx context.Context, req GetQuoteRequestObject) (GetQuoteResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.GetQuote(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	events := make([]InvoiceEvent, len(d.Events))
	for i, e := range d.Events {
		events[i] = InvoiceEvent{Action: e.Action, Actor: e.ActorName, Detail: e.Summary, At: e.At.UTC()}
	}
	emails := make([]EmailMessage, len(d.Emails))
	for i, m := range d.Emails {
		emails[i] = toAPIEmail(m)
	}
	out := GetQuote200JSONResponse{Quote: toAPIQuote(d.Quote, mu, d.Today), Events: events, Emails: emails, InvoiceNumber: d.InvoiceNumber}
	if d.Adhoc != nil {
		a := toAPIAdhocCustomer(*d.Adhoc)
		out.AdhocCustomer = &a
	}
	return out, nil
}

func (h *handlers) UpdateQuote(ctx context.Context, req UpdateQuoteRequestObject) (UpdateQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := h.svc.GetQuote(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	in, err := quoteInput(*req.Body, cur.Quote.Currency, mu)
	if err != nil {
		return nil, err
	}
	q, err := h.svc.UpdateQuote(ctx, req.Id, req.Body.Version, in)
	if err != nil {
		return nil, err
	}
	return UpdateQuote200JSONResponse(toAPIQuote(q, mu, today)), nil
}

func (h *handlers) DeleteQuote(ctx context.Context, req DeleteQuoteRequestObject) (DeleteQuoteResponseObject, error) {
	if err := h.svc.DeleteQuote(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteQuote204Response{}, nil
}

func (h *handlers) AcceptQuote(ctx context.Context, req AcceptQuoteRequestObject) (AcceptQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	q, err := h.svc.AcceptQuote(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return AcceptQuote200JSONResponse(toAPIQuote(q, mu, today)), nil
}

func (h *handlers) DeclineQuote(ctx context.Context, req DeclineQuoteRequestObject) (DeclineQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	reason := ""
	if req.Body != nil {
		reason = req.Body.Reason
	}
	q, err := h.svc.DeclineQuote(ctx, req.Id, reason)
	if err != nil {
		return nil, err
	}
	return DeclineQuote200JSONResponse(toAPIQuote(q, mu, today)), nil
}

func (h *handlers) ConvertQuote(ctx context.Context, req ConvertQuoteRequestObject) (ConvertQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.ConvertQuote(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return ConvertQuote201JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) SendQuote(ctx context.Context, req SendQuoteRequestObject) (SendQuoteResponseObject, error) {
	in := app.SendInput{IdempotencyKey: req.Params.IdempotencyKey}
	if b := req.Body; b != nil {
		in.To, in.CC, in.Subject, in.Body, in.LinkBase = b.To, b.Cc, b.Subject, b.Body, b.LinkBase
		if b.SendAt != nil {
			in.SendAt = b.SendAt.UTC()
		}
	}
	m, err := h.svc.SendQuote(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	return SendQuote202JSONResponse(toAPIEmail(*m)), nil
}

func (h *handlers) GetQuotePdf(ctx context.Context, req GetQuotePdfRequestObject) (GetQuotePdfResponseObject, error) {
	pdf, name, err := h.svc.QuotePDF(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetQuotePdf200ApplicationpdfResponse{
		Body: bytes.NewReader(pdf), ContentLength: int64(len(pdf)),
		Headers: GetQuotePdf200ResponseHeaders{ContentDisposition: `inline; filename="` + name + `"`},
	}, nil
}

func (h *handlers) RenewQuote(ctx context.Context, req RenewQuoteRequestObject) (RenewQuoteResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	q, err := h.svc.RenewQuote(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return RenewQuote201JSONResponse(toAPIQuote(q, mu, today)), nil
}

// ---- the customer's answer through a quote e-mail link (M41; no session) ----

func (h *handlers) toQuoteResponse(ctx context.Context, v app.QuoteResponseView) (QuoteResponse, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return QuoteResponse{}, err
	}
	q, c := v.Quote, v.Quote.Currency
	lines := make([]struct {
		Amount      string `json:"amount"`
		Description string `json:"description"`
		Quantity    string `json:"quantity"`
	}, len(q.Lines))
	for i, l := range q.Lines {
		lines[i].Description, lines[i].Quantity, lines[i].Amount = l.Description, l.Quantity.String(), mu.format(l.Amount(), c)
	}
	return QuoteResponse{
		Number: q.Number, BusinessName: v.Business.Name, BusinessEmail: v.Business.Email, BillToName: q.BillTo.Name,
		Currency: string(c), IssueDate: toAPIDate(q.IssueDate), ValidUntil: toAPIDate(q.ValidUntil),
		Total: mu.format(q.Totals.Total, c), Stage: QuoteStage(v.Stage), Expired: v.Expired,
		CanAnswer:  !v.Expired && (q.Status == domain.QuoteDraft || q.Status == domain.QuoteSent),
		AnsweredBy: QuoteResponseAnsweredBy(q.AnsweredBy), DeclineReason: q.DeclineReason, Lines: lines,
	}, nil
}

func (h *handlers) GetQuoteResponse(ctx context.Context, req GetQuoteResponseRequestObject) (GetQuoteResponseResponseObject, error) {
	v, err := h.svc.GetQuoteResponse(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	out, err := h.toQuoteResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return GetQuoteResponse200JSONResponse(out), nil
}

func (h *handlers) RespondToQuote(ctx context.Context, req RespondToQuoteRequestObject) (RespondToQuoteResponseObject, error) {
	answer, err := domain.ParseQuoteAnswer(string(req.Body.Answer))
	if err != nil {
		return nil, err
	}
	v, err := h.svc.RespondToQuote(ctx, req.Token, answer, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	out, err := h.toQuoteResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return RespondToQuote200JSONResponse(out), nil
}

func (h *handlers) GetQuoteResponsePdf(ctx context.Context, req GetQuoteResponsePdfRequestObject) (GetQuoteResponsePdfResponseObject, error) {
	pdf, name, err := h.svc.QuoteResponsePDF(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return GetQuoteResponsePdf200ApplicationpdfResponse{
		Body: bytes.NewReader(pdf), ContentLength: int64(len(pdf)),
		Headers: GetQuoteResponsePdf200ResponseHeaders{ContentDisposition: `inline; filename="` + name + `"`},
	}, nil
}

// ---- ad-hoc customers ----

func (h *handlers) ListAdhocCustomers(ctx context.Context, req ListAdhocCustomersRequestObject) (ListAdhocCustomersResponseObject, error) {
	p := req.Params
	as, total, err := h.svc.ListAdhocCustomers(ctx, ports.AdhocCustomerFilter{Query: p.Q, IncludeConverted: p.IncludeConverted, Page: page(p.Limit, p.Offset)})
	if err != nil {
		return nil, err
	}
	out := make([]AdhocCustomer, len(as))
	for i, a := range as {
		out[i] = toAPIAdhocCustomer(a)
	}
	return ListAdhocCustomers200JSONResponse{Items: out, Total: total}, nil
}

func (h *handlers) CreateAdhocCustomer(ctx context.Context, req CreateAdhocCustomerRequestObject) (CreateAdhocCustomerResponseObject, error) {
	a, err := h.svc.CreateAdhocCustomer(ctx, fromAPIAdhocCustomer("", *req.Body))
	if err != nil {
		return nil, err
	}
	return CreateAdhocCustomer201JSONResponse(toAPIAdhocCustomer(a)), nil
}

func (h *handlers) GetAdhocCustomer(ctx context.Context, req GetAdhocCustomerRequestObject) (GetAdhocCustomerResponseObject, error) {
	a, err := h.svc.GetAdhocCustomer(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetAdhocCustomer200JSONResponse(toAPIAdhocCustomer(a)), nil
}

func (h *handlers) UpdateAdhocCustomer(ctx context.Context, req UpdateAdhocCustomerRequestObject) (UpdateAdhocCustomerResponseObject, error) {
	a, err := h.svc.UpdateAdhocCustomer(ctx, fromAPIAdhocCustomer(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateAdhocCustomer200JSONResponse(toAPIAdhocCustomer(a)), nil
}

func (h *handlers) DeleteAdhocCustomer(ctx context.Context, req DeleteAdhocCustomerRequestObject) (DeleteAdhocCustomerResponseObject, error) {
	if err := h.svc.DeleteAdhocCustomer(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteAdhocCustomer204Response{}, nil
}

func (h *handlers) ConvertAdhocCustomer(ctx context.Context, req ConvertAdhocCustomerRequestObject) (ConvertAdhocCustomerResponseObject, error) {
	b := req.Body
	d := domain.CustomerDetails{Code: b.Code, Email: b.Email, Currency: domain.CurrencyCode(strings.ToUpper(b.Currency))}
	if b.PaymentTerms != nil {
		d.Terms = fromAPITerms(*b.PaymentTerms)
	}
	c, err := h.svc.ConvertAdhocCustomer(ctx, req.Id, d)
	if err != nil {
		return nil, err
	}
	return ConvertAdhocCustomer201JSONResponse(toAPICustomer(c)), nil
}

// ---- Settings → Quotes ----

func toAPIQuoteSettings(v app.QuoteSettingsView) QuoteSettings {
	s := v.Settings
	return QuoteSettings{
		ValidDays: s.ValidDays, NumberFormat: s.NumberFormat, EmailSubject: s.EmailSubject, EmailBody: s.EmailBody,
		Terms: s.Terms, PublicUrl: s.PublicURL, NextSequence: v.NextSequence, Version: s.Version,
	}
}

func (h *handlers) GetQuoteSettings(ctx context.Context, _ GetQuoteSettingsRequestObject) (GetQuoteSettingsResponseObject, error) {
	v, err := h.svc.GetQuoteSettings(ctx)
	if err != nil {
		return nil, err
	}
	return GetQuoteSettings200JSONResponse(toAPIQuoteSettings(v)), nil
}

func (h *handlers) UpdateQuoteSettings(ctx context.Context, req UpdateQuoteSettingsRequestObject) (UpdateQuoteSettingsResponseObject, error) {
	b := req.Body
	v, err := h.svc.UpdateQuoteSettings(ctx, domain.QuoteSettings{
		ValidDays: b.ValidDays, NumberFormat: b.NumberFormat, EmailSubject: b.EmailSubject, EmailBody: b.EmailBody,
		Terms: b.Terms, PublicURL: b.PublicUrl, Version: b.Version,
	}, b.NextSequence)
	if err != nil {
		return nil, err
	}
	return UpdateQuoteSettings200JSONResponse(toAPIQuoteSettings(v)), nil
}
