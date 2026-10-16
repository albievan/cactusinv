package httpapi

import (
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// handlers implements the generated StrictServerInterface by translating between the
// wire types and the application's use cases. No business rules live here.
type handlers struct {
	svc       *app.Service
	clock     ports.Clock
	cookies   cookieConfig
	version   string
	build     string
	maxUpload int64
}

var _ StrictServerInterface = (*handlers)(nil)

func (h *handlers) units(ctx context.Context) (minorUnits, error) {
	cs, err := h.svc.ListCurrencies(ctx)
	if err != nil {
		return nil, err
	}
	mu := minorUnits{}
	for _, c := range cs {
		mu[c.Code] = c.MinorUnits
	}
	return mu, nil
}

func page(limit, offset int) ports.Page { return ports.Page{Limit: limit, Offset: offset}.Normalize() }

// ---- auth ----

func (h *handlers) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	token, exp, err := h.svc.Login(ctx, req.Body.Email, req.Body.Password)
	if err != nil {
		return nil, err
	}
	return Login204Response{Headers: Login204ResponseHeaders{SetCookie: h.cookies.session(token, exp, h.clock.Now()).String()}}, nil
}

func (h *handlers) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if err := h.svc.Logout(ctx, sessionToken(ctx)); err != nil {
		return nil, err
	}
	return Logout204Response{Headers: Logout204ResponseHeaders{SetCookie: h.cookies.cleared().String()}}, nil
}

func (h *handlers) ChangePassword(ctx context.Context, req ChangePasswordRequestObject) (ChangePasswordResponseObject, error) {
	if err := h.svc.ChangePassword(ctx, currentUser(ctx).ID, sessionToken(ctx), req.Body.CurrentPassword, req.Body.NewPassword); err != nil {
		return nil, err
	}
	return ChangePassword204Response{}, nil
}

func (h *handlers) Me(ctx context.Context, _ MeRequestObject) (MeResponseObject, error) {
	u := currentUser(ctx)
	held, err := h.svc.AutomationHeld(ctx)
	if err != nil {
		return nil, err
	}
	perms := u.Access.Permissions()
	out := Me200JSONResponse{
		Id: u.ID, Email: u.Email, AutomationPaused: held, MustChangePassword: u.MustChangePassword,
		Administrator: u.Access.Administrator, Permissions: make([]string, len(perms)),
	}
	for i, p := range perms {
		out.Permissions[i] = string(p)
	}
	return out, nil
}

// ---- settings ----

func toAPIBusiness(s app.BusinessSettings) BusinessSettings {
	p := s.Profile
	return BusinessSettings{
		Name: p.Name, AddressLines: p.AddressLines, Phone: p.Phone, Email: p.Email, TaxNumber: p.TaxNumber,
		NumberFormat: p.NumberFormat, TimeZone: p.TimeZone, DefaultNotes: p.DefaultNotes,
		EmailSubject: p.EmailSubject, EmailBody: p.EmailBody, VatPercent: p.VATRate.String(),
		NextSequence: s.NextSequence, Version: p.Version,
	}
}

func (h *handlers) GetBusiness(ctx context.Context, _ GetBusinessRequestObject) (GetBusinessResponseObject, error) {
	s, err := h.svc.GetBusiness(ctx)
	if err != nil {
		return nil, err
	}
	return GetBusiness200JSONResponse(toAPIBusiness(s)), nil
}

func (h *handlers) UpdateBusiness(ctx context.Context, req UpdateBusinessRequestObject) (UpdateBusinessResponseObject, error) {
	b := req.Body
	vat, err := percent("vatPercent", b.VatPercent)
	if err != nil {
		return nil, err
	}
	s, err := h.svc.UpdateBusiness(ctx, domain.BusinessProfile{
		Name: b.Name, AddressLines: b.AddressLines, Phone: b.Phone, Email: b.Email, TaxNumber: b.TaxNumber,
		NumberFormat: b.NumberFormat, TimeZone: b.TimeZone, DefaultNotes: b.DefaultNotes,
		EmailSubject: b.EmailSubject, EmailBody: b.EmailBody, VATRate: vat, Version: b.Version,
	}, b.NextSequence)
	if err != nil {
		return nil, err
	}
	return UpdateBusiness200JSONResponse(toAPIBusiness(s)), nil
}

// ---- currencies ----

func (h *handlers) ListCurrencies(ctx context.Context, _ ListCurrenciesRequestObject) (ListCurrenciesResponseObject, error) {
	cs, err := h.svc.ListCurrencies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Currency, len(cs))
	for i, c := range cs {
		out[i] = toAPICurrency(c)
	}
	return ListCurrencies200JSONResponse{Items: out}, nil
}

func (h *handlers) CreateCurrency(ctx context.Context, req CreateCurrencyRequestObject) (CreateCurrencyResponseObject, error) {
	b := req.Body
	c, err := h.svc.CreateCurrency(ctx, b.Code, b.Name, b.Symbol, b.MinorUnits, b.Active)
	if err != nil {
		return nil, err
	}
	return CreateCurrency201JSONResponse(toAPICurrency(c)), nil
}

func (h *handlers) UpdateCurrency(ctx context.Context, req UpdateCurrencyRequestObject) (UpdateCurrencyResponseObject, error) {
	code, err := domain.ParseCurrencyCode(req.Code)
	if err != nil {
		return nil, err
	}
	c, err := h.svc.UpdateCurrency(ctx, code, req.Body.Name, req.Body.Symbol, req.Body.Active)
	if err != nil {
		return nil, err
	}
	return UpdateCurrency200JSONResponse(toAPICurrency(c)), nil
}

// ---- bank accounts ----

func (h *handlers) ListBankAccounts(ctx context.Context, req ListBankAccountsRequestObject) (ListBankAccountsResponseObject, error) {
	bs, err := h.svc.ListBankAccounts(ctx, domain.CurrencyCode(strings.ToUpper(req.Params.Currency)))
	if err != nil {
		return nil, err
	}
	out := make([]BankAccount, len(bs))
	for i, b := range bs {
		out[i] = toAPIBank(b)
	}
	return ListBankAccounts200JSONResponse{Items: out}, nil
}

func (h *handlers) CreateBankAccount(ctx context.Context, req CreateBankAccountRequestObject) (CreateBankAccountResponseObject, error) {
	b, err := h.svc.CreateBankAccount(ctx, fromAPIBank("", *req.Body))
	if err != nil {
		return nil, err
	}
	return CreateBankAccount201JSONResponse(toAPIBank(b)), nil
}

func (h *handlers) GetBankAccount(ctx context.Context, req GetBankAccountRequestObject) (GetBankAccountResponseObject, error) {
	b, err := h.svc.GetBankAccount(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetBankAccount200JSONResponse(toAPIBank(b)), nil
}

func (h *handlers) UpdateBankAccount(ctx context.Context, req UpdateBankAccountRequestObject) (UpdateBankAccountResponseObject, error) {
	b, err := h.svc.UpdateBankAccount(ctx, fromAPIBank(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateBankAccount200JSONResponse(toAPIBank(b)), nil
}

func (h *handlers) DeleteBankAccount(ctx context.Context, req DeleteBankAccountRequestObject) (DeleteBankAccountResponseObject, error) {
	if err := h.svc.DeleteBankAccount(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteBankAccount204Response{}, nil
}

// ---- customers ----

func (h *handlers) ListCustomers(ctx context.Context, req ListCustomersRequestObject) (ListCustomersResponseObject, error) {
	p := req.Params
	cs, total, err := h.svc.ListCustomers(ctx, ports.CustomerFilter{Query: p.Q, IncludeInactive: p.IncludeInactive, Page: page(p.Limit, p.Offset)})
	if err != nil {
		return nil, err
	}
	out := make([]Customer, len(cs))
	for i, c := range cs {
		out[i] = toAPICustomer(c)
	}
	return ListCustomers200JSONResponse{Items: out, Total: total}, nil
}

func (h *handlers) CreateCustomer(ctx context.Context, req CreateCustomerRequestObject) (CreateCustomerResponseObject, error) {
	c, err := h.svc.CreateCustomer(ctx, fromAPICustomer("", *req.Body))
	if err != nil {
		return nil, err
	}
	return CreateCustomer201JSONResponse(toAPICustomer(c)), nil
}

func (h *handlers) GetCustomer(ctx context.Context, req GetCustomerRequestObject) (GetCustomerResponseObject, error) {
	c, err := h.svc.GetCustomer(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetCustomer200JSONResponse(toAPICustomer(c)), nil
}

func (h *handlers) UpdateCustomer(ctx context.Context, req UpdateCustomerRequestObject) (UpdateCustomerResponseObject, error) {
	c, err := h.svc.UpdateCustomer(ctx, fromAPICustomer(req.Id, *req.Body))
	if err != nil {
		return nil, err
	}
	return UpdateCustomer200JSONResponse(toAPICustomer(c)), nil
}

func (h *handlers) DeleteCustomer(ctx context.Context, req DeleteCustomerRequestObject) (DeleteCustomerResponseObject, error) {
	if err := h.svc.DeleteCustomer(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteCustomer204Response{}, nil
}

// ---- products ----

func (h *handlers) ListProducts(ctx context.Context, req ListProductsRequestObject) (ListProductsResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	ps, total, err := h.svc.ListProducts(ctx, ports.ProductFilter{
		Query: p.Q, Currency: domain.CurrencyCode(strings.ToUpper(p.Currency)), IncludeInactive: p.IncludeInactive,
		Page: page(p.Limit, p.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Product, len(ps))
	for i, x := range ps {
		out[i] = toAPIProduct(x, mu)
	}
	return ListProducts200JSONResponse{Items: out, Total: total}, nil
}

func (h *handlers) productInput(ctx context.Context, id string, body Product) (domain.Product, minorUnits, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return domain.Product{}, nil, err
	}
	p, err := fromAPIProduct(id, body, mu)
	return p, mu, err
}

func (h *handlers) CreateProduct(ctx context.Context, req CreateProductRequestObject) (CreateProductResponseObject, error) {
	in, mu, err := h.productInput(ctx, "", *req.Body)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.CreateProduct(ctx, in)
	if err != nil {
		return nil, err
	}
	return CreateProduct201JSONResponse(toAPIProduct(p, mu)), nil
}

func (h *handlers) GetProduct(ctx context.Context, req GetProductRequestObject) (GetProductResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.GetProduct(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetProduct200JSONResponse(toAPIProduct(p, mu)), nil
}

func (h *handlers) UpdateProduct(ctx context.Context, req UpdateProductRequestObject) (UpdateProductResponseObject, error) {
	in, mu, err := h.productInput(ctx, req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	p, err := h.svc.UpdateProduct(ctx, in)
	if err != nil {
		return nil, err
	}
	return UpdateProduct200JSONResponse(toAPIProduct(p, mu)), nil
}

func (h *handlers) DeleteProduct(ctx context.Context, req DeleteProductRequestObject) (DeleteProductResponseObject, error) {
	if err := h.svc.DeleteProduct(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteProduct204Response{}, nil
}

// ---- invoices ----

// invoiceView loads what is needed to render invoices: minor units and today's date.
func (h *handlers) invoiceView(ctx context.Context) (minorUnits, domain.Date, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, domain.Date{}, err
	}
	today, err := h.svc.Today(ctx)
	return mu, today, err
}

func (h *handlers) ListInvoices(ctx context.Context, req ListInvoicesRequestObject) (ListInvoicesResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	invs, total, err := h.svc.ListInvoices(ctx, ports.InvoiceFilter{
		Status: domain.InvoiceStatus(p.Status), CustomerID: p.CustomerId, Query: p.Q, Overdue: p.Overdue,
		Page: page(p.Limit, p.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Invoice, len(invs))
	for i := range invs {
		out[i] = toAPIInvoice(&invs[i], mu, today)
	}
	return ListInvoices200JSONResponse{Items: out, Total: total}, nil
}

// invoiceInput parses an InvoiceInput; amounts are in currency.
func invoiceInput(b InvoiceInput, currency domain.CurrencyCode, mu minorUnits) (app.InvoiceInput, error) {
	tax, err := optionalPercent("vatPercent", b.VatPercent)
	if err != nil {
		return app.InvoiceInput{}, err
	}
	in := app.InvoiceInput{
		CustomerID: b.CustomerId, IssueDate: fromAPIDatePtr(b.IssueDate), Header: fromAPIHeader(b.Header),
		BillTo: fromAPIPartyPtr(b.BillTo), ShipTo: fromAPIPartyPtr(b.ShipTo), Notes: b.Notes, TaxRate: tax,
	}
	if b.PaymentTerms != nil {
		t := fromAPITerms(*b.PaymentTerms)
		in.Terms = &t
	}
	for _, l := range b.Lines {
		q, err := quantity("lines.quantity", l.Quantity)
		if err != nil {
			return app.InvoiceInput{}, err
		}
		disc, err := percent("lines.discountPercent", l.DiscountPercent)
		if err != nil {
			return app.InvoiceInput{}, err
		}
		li := app.LineInput{ProductID: l.ProductId, Quantity: q, Discount: disc}
		if l.UnitPrice != nil {
			v, err := mu.parse("lines.unitPrice", *l.UnitPrice, currency)
			if err != nil {
				return app.InvoiceInput{}, err
			}
			li.UnitPrice = &v
		}
		in.Lines = append(in.Lines, li)
	}
	return in, nil
}

func (h *handlers) CreateInvoice(ctx context.Context, req CreateInvoiceRequestObject) (CreateInvoiceResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body.CustomerId == "" {
		return nil, &domain.ValidationError{Field: "customerId", Reason: "is required"}
	}
	c, err := h.svc.GetCustomer(ctx, req.Body.CustomerId)
	if err != nil {
		return nil, err
	}
	in, err := invoiceInput(*req.Body, c.Currency, mu)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.CreateInvoice(ctx, in)
	if err != nil {
		return nil, err
	}
	return CreateInvoice201JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) GetInvoice(ctx context.Context, req GetInvoiceRequestObject) (GetInvoiceResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.GetInvoice(ctx, req.Id)
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
	return GetInvoice200JSONResponse{Invoice: toAPIInvoice(d.Invoice, mu, today), Events: events, Emails: emails}, nil
}

func (h *handlers) UpdateInvoice(ctx context.Context, req UpdateInvoiceRequestObject) (UpdateInvoiceResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	cur, err := h.svc.GetInvoice(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	in, err := invoiceInput(*req.Body, cur.Invoice.Currency, mu)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.UpdateInvoice(ctx, req.Id, req.Body.Version, in)
	if err != nil {
		return nil, err
	}
	return UpdateInvoice200JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) DeleteInvoice(ctx context.Context, req DeleteInvoiceRequestObject) (DeleteInvoiceResponseObject, error) {
	if err := h.svc.DeleteInvoice(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteInvoice204Response{}, nil
}

func (h *handlers) IssueInvoice(ctx context.Context, req IssueInvoiceRequestObject) (IssueInvoiceResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	var date domain.Date
	if req.Body != nil {
		date = fromAPIDatePtr(req.Body.IssueDate)
	}
	inv, err := h.svc.IssueInvoice(ctx, req.Id, date)
	if err != nil {
		return nil, err
	}
	return IssueInvoice200JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) VoidInvoice(ctx context.Context, req VoidInvoiceRequestObject) (VoidInvoiceResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.VoidInvoice(ctx, req.Id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return VoidInvoice200JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) MarkInvoiceOverdue(ctx context.Context, req MarkInvoiceOverdueRequestObject) (MarkInvoiceOverdueResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.MarkInvoiceOverdue(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return MarkInvoiceOverdue200JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) MarkInvoicePaid(ctx context.Context, req MarkInvoicePaidRequestObject) (MarkInvoicePaidResponseObject, error) {
	mu, today, err := h.invoiceView(ctx)
	if err != nil {
		return nil, err
	}
	inv, err := h.svc.MarkInvoicePaid(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return MarkInvoicePaid200JSONResponse(toAPIInvoice(inv, mu, today)), nil
}

func (h *handlers) SendInvoice(ctx context.Context, req SendInvoiceRequestObject) (SendInvoiceResponseObject, error) {
	in := app.SendInput{IdempotencyKey: req.Params.IdempotencyKey}
	if b := req.Body; b != nil {
		in.To, in.CC, in.Subject, in.Body = b.To, b.Cc, b.Subject, b.Body
		if b.SendAt != nil {
			in.SendAt = b.SendAt.UTC()
		}
	}
	m, err := h.svc.SendInvoice(ctx, req.Id, in)
	if err != nil {
		return nil, err
	}
	return SendInvoice202JSONResponse(toAPIEmail(*m)), nil
}

func (h *handlers) GetInvoicePdf(ctx context.Context, req GetInvoicePdfRequestObject) (GetInvoicePdfResponseObject, error) {
	pdf, name, err := h.svc.InvoicePDF(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return GetInvoicePdf200ApplicationpdfResponse{
		Body: bytes.NewReader(pdf), ContentLength: int64(len(pdf)),
		Headers: GetInvoicePdf200ResponseHeaders{ContentDisposition: `inline; filename="` + name + `"`},
	}, nil
}

// ---- e-mails ----

func (h *handlers) ListEmails(ctx context.Context, req ListEmailsRequestObject) (ListEmailsResponseObject, error) {
	p := req.Params
	ms, total, err := h.svc.ListEmails(ctx, ports.EmailFilter{Status: domain.EmailStatus(p.Status), InvoiceID: p.InvoiceId, CustomerID: p.CustomerId, Page: page(p.Limit, p.Offset)})
	if err != nil {
		return nil, err
	}
	out := make([]EmailMessage, len(ms))
	for i, m := range ms {
		out[i] = toAPIEmail(m)
	}
	return ListEmails200JSONResponse{Items: out, Total: total}, nil
}

func (h *handlers) CancelEmail(ctx context.Context, req CancelEmailRequestObject) (CancelEmailResponseObject, error) {
	m, err := h.svc.CancelEmail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return CancelEmail200JSONResponse(toAPIEmail(*m)), nil
}

func (h *handlers) RetryEmail(ctx context.Context, req RetryEmailRequestObject) (RetryEmailResponseObject, error) {
	m, err := h.svc.RetryEmail(ctx, req.Id)
	if err != nil {
		return nil, err
	}
	return RetryEmail200JSONResponse(toAPIEmail(*m)), nil
}

// ---- schedules ----

// scheduleCurrencies resolves the billing currency of each schedule's customer.
type scheduleCurrencies struct {
	h     *handlers
	cache map[string]domain.CurrencyCode
}

func (s *scheduleCurrencies) of(ctx context.Context, customerID string) (domain.CurrencyCode, error) {
	if c, ok := s.cache[customerID]; ok {
		return c, nil
	}
	cust, err := s.h.svc.GetCustomer(ctx, customerID)
	if err != nil {
		return "", err
	}
	s.cache[customerID] = cust.Currency
	return cust.Currency, nil
}

func (h *handlers) currencies() *scheduleCurrencies {
	return &scheduleCurrencies{h: h, cache: map[string]domain.CurrencyCode{}}
}

func (h *handlers) scheduleDetail(ctx context.Context, d app.ScheduleDetail, mu minorUnits) (ScheduleDetail, error) {
	cur, err := h.currencies().of(ctx, d.Schedule.CustomerID)
	if err != nil {
		return ScheduleDetail{}, err
	}
	upcoming := make([]time.Time, len(d.Upcoming))
	for i, t := range d.Upcoming {
		upcoming[i] = t.UTC()
	}
	runs := make([]ScheduleRun, len(d.Runs))
	for i, r := range d.Runs {
		runs[i] = ScheduleRun{Period: toAPIDate(r.Period), InvoiceId: r.InvoiceID, At: r.At.UTC()}
	}
	return ScheduleDetail{Schedule: toAPISchedule(d.Schedule, d.LineProducts, cur, mu), Upcoming: upcoming, Runs: runs}, nil
}

func (h *handlers) ListSchedules(ctx context.Context, req ListSchedulesRequestObject) (ListSchedulesResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	ss, total, err := h.svc.ListSchedules(ctx, ports.ScheduleFilter{CustomerID: p.CustomerId, Status: domain.ScheduleStatus(p.Status), Page: page(p.Limit, p.Offset)})
	if err != nil {
		return nil, err
	}
	curs := h.currencies()
	out := make([]Schedule, len(ss))
	for i := range ss {
		c, err := curs.of(ctx, ss[i].CustomerID)
		if err != nil {
			return nil, err
		}
		out[i] = toAPISchedule(&ss[i].Schedule, ss[i].LineProducts, c, mu)
	}
	return ListSchedules200JSONResponse{Items: out, Total: total}, nil
}

func (h *handlers) scheduleInput(ctx context.Context, id string, body Schedule) (domain.Schedule, minorUnits, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return domain.Schedule{}, nil, err
	}
	if body.CustomerId == "" {
		return domain.Schedule{}, nil, &domain.ValidationError{Field: "customerId", Reason: "is required"}
	}
	cur, err := h.currencies().of(ctx, body.CustomerId)
	if err != nil {
		return domain.Schedule{}, nil, err
	}
	s, err := fromAPISchedule(id, body, cur, mu)
	return s, mu, err
}

func (h *handlers) CreateSchedule(ctx context.Context, req CreateScheduleRequestObject) (CreateScheduleResponseObject, error) {
	in, mu, err := h.scheduleInput(ctx, "", *req.Body)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.CreateSchedule(ctx, in)
	if err != nil {
		return nil, err
	}
	out, err := h.scheduleDetail(ctx, d, mu)
	if err != nil {
		return nil, err
	}
	return CreateSchedule201JSONResponse(out), nil
}

func (h *handlers) PreviewSchedule(ctx context.Context, req PreviewScheduleRequestObject) (PreviewScheduleResponseObject, error) {
	r, err := fromAPIRecurrence(*req.Body)
	if err != nil {
		return nil, err
	}
	pv, err := h.svc.PreviewRecurrence(ctx, r, 6)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, len(pv.Runs))
	for i, t := range pv.Runs {
		out[i] = t.UTC()
	}
	return PreviewSchedule200JSONResponse{Runs: out, TimeZone: pv.TimeZone, WeekendRule: RunPreviewWeekendRule(pv.Weekend)}, nil
}

func toAPIEmailTemplate(t domain.EmailTemplate) EmailTemplate {
	return EmailTemplate{Subject: t.Subject, Body: t.Body, Placeholders: t.Placeholders}
}

func (h *handlers) GetScheduleDefaults(ctx context.Context, _ GetScheduleDefaultsRequestObject) (GetScheduleDefaultsResponseObject, error) {
	d, err := h.svc.ScheduleEmailDefaults(ctx)
	if err != nil {
		return nil, err
	}
	return GetScheduleDefaults200JSONResponse{
		Invoice: toAPIEmailTemplate(d.Invoice), Unsent: toAPIEmailTemplate(d.Unsent),
		Outstanding: toAPIEmailTemplate(d.Outstanding), Statement: toAPIEmailTemplate(d.Statement),
	}, nil
}

func (h *handlers) GetSchedule(ctx context.Context, req GetScheduleRequestObject) (GetScheduleResponseObject, error) {
	return withSchedule(ctx, h, func() (app.ScheduleDetail, error) { return h.svc.GetSchedule(ctx, req.Id) },
		func(d ScheduleDetail) GetScheduleResponseObject { return GetSchedule200JSONResponse(d) })
}

func (h *handlers) UpdateSchedule(ctx context.Context, req UpdateScheduleRequestObject) (UpdateScheduleResponseObject, error) {
	in, _, err := h.scheduleInput(ctx, req.Id, *req.Body)
	if err != nil {
		return nil, err
	}
	return withSchedule(ctx, h, func() (app.ScheduleDetail, error) { return h.svc.UpdateSchedule(ctx, in) },
		func(d ScheduleDetail) UpdateScheduleResponseObject { return UpdateSchedule200JSONResponse(d) })
}

func (h *handlers) DeleteSchedule(ctx context.Context, req DeleteScheduleRequestObject) (DeleteScheduleResponseObject, error) {
	if err := h.svc.DeleteSchedule(ctx, req.Id); err != nil {
		return nil, err
	}
	return DeleteSchedule204Response{}, nil
}

func (h *handlers) PauseSchedule(ctx context.Context, req PauseScheduleRequestObject) (PauseScheduleResponseObject, error) {
	return withSchedule(ctx, h, func() (app.ScheduleDetail, error) { return h.svc.PauseSchedule(ctx, req.Id) },
		func(d ScheduleDetail) PauseScheduleResponseObject { return PauseSchedule200JSONResponse(d) })
}

func (h *handlers) ResumeSchedule(ctx context.Context, req ResumeScheduleRequestObject) (ResumeScheduleResponseObject, error) {
	return withSchedule(ctx, h, func() (app.ScheduleDetail, error) { return h.svc.ResumeSchedule(ctx, req.Id) },
		func(d ScheduleDetail) ResumeScheduleResponseObject { return ResumeSchedule200JSONResponse(d) })
}

func (h *handlers) RunScheduleNow(ctx context.Context, req RunScheduleNowRequestObject) (RunScheduleNowResponseObject, error) {
	return withSchedule(ctx, h, func() (app.ScheduleDetail, error) { return h.svc.RunScheduleNow(ctx, req.Id) },
		func(d ScheduleDetail) RunScheduleNowResponseObject { return RunScheduleNow200JSONResponse(d) })
}

// withSchedule runs a schedule use case and renders its detail response.
func withSchedule[R any](ctx context.Context, h *handlers, run func() (app.ScheduleDetail, error), wrapResp func(ScheduleDetail) R) (R, error) {
	var zero R
	mu, err := h.units(ctx)
	if err != nil {
		return zero, err
	}
	d, err := run()
	if err != nil {
		return zero, err
	}
	out, err := h.scheduleDetail(ctx, d, mu)
	if err != nil {
		return zero, err
	}
	return wrapResp(out), nil
}

// ---- dashboard ----

func (h *handlers) GetDashboard(ctx context.Context, _ GetDashboardRequestObject) (GetDashboardResponseObject, error) {
	mu, err := h.units(ctx)
	if err != nil {
		return nil, err
	}
	d, err := h.svc.GetDashboard(ctx)
	if err != nil {
		return nil, err
	}
	out := GetDashboard200JSONResponse{
		Today: toAPIDate(d.Today), FailedEmailCount: d.FailedEmailCount,
		Totals: []CurrencyTotal{}, UpcomingEmails: []EmailMessage{}, UpcomingRuns: []Schedule{}, RecentInvoices: []Invoice{},
		Months: d.Months, Invoiced: []MonthTotal{}, Paid: []MonthTotal{}, Activity: []AuditEntry{}, TopDebtors: []Debtor{},
	}
	for _, m := range d.Invoiced {
		out.Invoiced = append(out.Invoiced, MonthTotal{Currency: string(m.Currency), Month: m.Month, Total: mu.format(m.Total, m.Currency), Count: m.Count})
	}
	for _, m := range d.Paid {
		out.Paid = append(out.Paid, MonthTotal{Currency: string(m.Currency), Month: m.Month, Total: mu.format(m.Total, m.Currency), Count: m.Count})
	}
	for _, t := range d.TopDebtors {
		out.TopDebtors = append(out.TopDebtors, Debtor{Currency: string(t.Currency), CustomerId: t.CustomerID, Name: t.Name, OpenCount: t.OpenCount, Outstanding: mu.format(t.Outstanding, t.Currency)})
	}
	for _, e := range d.Activity {
		out.Activity = append(out.Activity, AuditEntry{Seq: e.Seq, At: e.At.UTC(), ActorId: e.ActorID, Actor: e.ActorName, Action: e.Action, EntityType: e.EntityType, EntityId: e.EntityID, Summary: e.Summary, Hash: e.Hash})
	}
	for _, t := range d.Totals {
		out.Totals = append(out.Totals, CurrencyTotal{
			Currency: string(t.Currency), OpenCount: t.OpenCount, Outstanding: mu.format(t.Outstanding, t.Currency),
			OverdueCount: t.OverdueCount, Overdue: mu.format(t.Overdue, t.Currency),
		})
	}
	for _, m := range d.UpcomingEmails {
		out.UpcomingEmails = append(out.UpcomingEmails, toAPIEmail(m))
	}
	curs := h.currencies()
	for i := range d.UpcomingRuns {
		c, err := curs.of(ctx, d.UpcomingRuns[i].CustomerID)
		if err != nil {
			return nil, err
		}
		out.UpcomingRuns = append(out.UpcomingRuns, toAPISchedule(&d.UpcomingRuns[i], nil, c, mu))
	}
	for i := range d.RecentInvoices {
		out.RecentInvoices = append(out.RecentInvoices, toAPIInvoice(&d.RecentInvoices[i], mu, d.Today))
	}
	return out, nil
}
