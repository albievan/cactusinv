package httpapi

import (
	"fmt"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/albievan/cactusinve/internal/domain"
)

// minorUnits maps currency codes to their minor units, for formatting and parsing money.
type minorUnits map[domain.CurrencyCode]int

func (m minorUnits) of(c domain.CurrencyCode) int {
	if v, ok := m[c]; ok {
		return v
	}
	return 2
}

func (m minorUnits) format(amount int64, c domain.CurrencyCode) string {
	return domain.FormatAmount(amount, m.of(c))
}

func (m minorUnits) parse(field, s string, c domain.CurrencyCode) (int64, error) {
	if _, ok := m[c]; !ok {
		return 0, &domain.ValidationError{Field: field, Reason: "unknown currency " + string(c)}
	}
	v, err := domain.ParseAmount(s, m[c])
	if err != nil {
		return 0, &domain.ValidationError{Field: field, Reason: "must be an amount with at most " + fmt.Sprint(m[c]) + " decimals"}
	}
	return v, nil
}

func toAPIDate(d domain.Date) openapi_types.Date { return openapi_types.Date{Time: d.Time(time.UTC)} }

func toAPIDatePtr(d domain.Date) *openapi_types.Date {
	if d.IsZero() {
		return nil
	}
	v := toAPIDate(d)
	return &v
}

func fromAPIDate(d openapi_types.Date) domain.Date {
	if d.IsZero() {
		return domain.Date{}
	}
	return domain.DateOf(d.Time)
}

func fromAPIDatePtr(d *openapi_types.Date) domain.Date {
	if d == nil {
		return domain.Date{}
	}
	return fromAPIDate(*d)
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func percent(field, s string) (domain.BasisPoints, error) {
	if strings.TrimSpace(s) == "" {
		return 0, nil
	}
	v, err := domain.ParsePercent(s)
	if err != nil {
		return 0, &domain.ValidationError{Field: field, Reason: "must be a percentage between 0 and 100"}
	}
	return v, nil
}

func quantity(field, s string) (domain.Quantity, error) {
	q, err := domain.ParseQuantity(s)
	if err != nil {
		return 0, &domain.ValidationError{Field: field, Reason: "must be a number with at most 3 decimals"}
	}
	return q, nil
}

// ---- currencies & bank accounts ----

func toAPICurrency(c domain.Currency) Currency {
	return Currency{Code: string(c.Code), Name: c.Name, Symbol: c.Symbol, MinorUnits: c.MinorUnits, Active: c.Active}
}

func toAPIBank(b domain.BankAccount) BankAccount {
	return BankAccount{
		Id: b.ID, Currency: string(b.Currency), Label: b.Label, BankName: b.BankName, Branch: b.Branch,
		BranchCode: b.BranchCode, AccountName: b.AccountName, AccountNumber: b.AccountNumber,
		AccountType: b.AccountType, SwiftBic: b.SwiftBIC, Iban: b.IBAN, IsDefault: b.IsDefault, Active: b.Active,
		Version: b.Version,
	}
}

func fromAPIBank(id string, b BankAccount) domain.BankAccount {
	return domain.BankAccount{
		ID: id, Currency: domain.CurrencyCode(strings.ToUpper(b.Currency)), Label: b.Label, BankName: b.BankName,
		Branch: b.Branch, BranchCode: b.BranchCode, AccountName: b.AccountName, AccountNumber: b.AccountNumber,
		AccountType: b.AccountType, SwiftBIC: b.SwiftBic, IBAN: b.Iban, IsDefault: b.IsDefault, Active: b.Active,
		Version: b.Version,
	}
}

// ---- customers ----

func toAPITerms(t domain.PaymentTerms) PaymentTerms {
	return PaymentTerms{Kind: PaymentTermsKind(t.Kind), Value: t.Value, Text: t.Text()}
}

func fromAPITerms(t PaymentTerms) domain.PaymentTerms {
	return domain.PaymentTerms{Kind: domain.TermsKind(t.Kind), Value: t.Value}
}

func toAPICustomer(c domain.Customer) Customer {
	cc := c.CC
	if cc == nil {
		cc = []string{}
	}
	return Customer{
		Id: c.ID, Code: c.Code, Name: c.Name, Email: c.Email, Cc: cc, Attention: c.Attention,
		BillingAddress: c.BillingAddress, ShippingName: c.ShippingName, ShippingAddress: c.ShippingAddress,
		Phone: c.Phone, TaxNumber: c.TaxNumber, Currency: string(c.Currency), PaymentTerms: toAPITerms(c.Terms),
		BankAccountId: c.BankAccountID, DefaultNotes: c.DefaultNotes, Active: c.Active, Version: c.Version,
	}
}

func fromAPICustomer(id string, c Customer) domain.Customer {
	return domain.Customer{
		ID: id, Code: c.Code, Name: c.Name, Email: c.Email, CC: c.Cc, Attention: c.Attention,
		BillingAddress: c.BillingAddress, ShippingName: c.ShippingName, ShippingAddress: c.ShippingAddress,
		Phone: c.Phone, TaxNumber: c.TaxNumber, Currency: domain.CurrencyCode(strings.ToUpper(c.Currency)),
		Terms: fromAPITerms(c.PaymentTerms), BankAccountID: c.BankAccountId, DefaultNotes: c.DefaultNotes,
		Active: c.Active, Version: c.Version,
	}
}

// ---- products ----

func toAPIProduct(p domain.Product, mu minorUnits) Product {
	prices := make([]Price, 0, len(p.Prices))
	for _, c := range p.Currencies() {
		prices = append(prices, Price{Currency: string(c), UnitPrice: mu.format(p.Prices[c], c)})
	}
	return Product{
		Id: p.ID, Sku: p.SKU, Name: p.Name, Description: p.Description, Unit: p.Unit, Prices: prices,
		Active: p.Active, Version: p.Version,
	}
}

func fromAPIProduct(id string, p Product, mu minorUnits) (domain.Product, error) {
	prices := map[domain.CurrencyCode]int64{}
	for _, pr := range p.Prices {
		c := domain.CurrencyCode(strings.ToUpper(pr.Currency))
		if _, dup := prices[c]; dup {
			return domain.Product{}, &domain.ValidationError{Field: "prices", Reason: "one price per currency"}
		}
		v, err := mu.parse("prices.unitPrice", pr.UnitPrice, c)
		if err != nil {
			return domain.Product{}, err
		}
		prices[c] = v
	}
	return domain.Product{
		ID: id, SKU: p.Sku, Name: p.Name, Description: p.Description, Unit: p.Unit, Prices: prices,
		Active: p.Active, Version: p.Version,
	}, nil
}

// ---- invoices ----

func toAPIParty(p domain.Party) Party {
	return Party{Name: p.Name, Address: p.Address, Attention: p.Attention}
}

func fromAPIPartyPtr(p *Party) *domain.Party {
	if p == nil {
		return nil
	}
	return &domain.Party{Name: p.Name, Address: p.Address, Attention: p.Attention}
}

func toAPIHeader(h domain.InvoiceHeader) InvoiceHeader {
	return InvoiceHeader{
		SalesPerson: h.SalesPerson, Job: h.Job, ShippingMethod: h.ShippingMethod, ShippingTerms: h.ShippingTerms,
		DeliveryDate: toAPIDatePtr(h.DeliveryDate),
	}
}

func fromAPIHeader(h InvoiceHeader) domain.InvoiceHeader {
	return domain.InvoiceHeader{
		SalesPerson: h.SalesPerson, Job: h.Job, ShippingMethod: h.ShippingMethod, ShippingTerms: h.ShippingTerms,
		DeliveryDate: fromAPIDatePtr(h.DeliveryDate),
	}
}

func toAPIInvoice(inv *domain.Invoice, mu minorUnits, today domain.Date) Invoice {
	c := inv.Currency
	lines := make([]Line, len(inv.Lines))
	for i, l := range inv.Lines {
		lines[i] = Line{
			ProductId: l.ProductID, Description: l.Description, Quantity: l.Quantity.String(),
			UnitPrice: mu.format(l.UnitPrice, c), DiscountPercent: l.Discount.String(), Amount: mu.format(l.Amount(), c),
		}
	}
	terms := toAPITerms(inv.Terms)
	if inv.TermsText != "" {
		terms.Text = inv.TermsText
	}
	b := inv.Bank
	return Invoice{
		Id: inv.ID, Number: inv.Number, Status: InvoiceStatus(inv.Status), CustomerId: inv.CustomerID,
		Currency: string(c), IssueDate: toAPIDate(inv.IssueDate), DueDate: toAPIDatePtr(inv.DueDate),
		PaymentTerms: terms, Header: toAPIHeader(inv.Header), BillTo: toAPIParty(inv.BillTo), ShipTo: toAPIParty(inv.ShipTo),
		Notes: inv.Notes, TaxPercent: inv.TaxRate.String(), Lines: lines,
		Totals: Totals{
			Subtotal: mu.format(inv.Totals.Subtotal, c), Discount: mu.format(inv.Totals.Discount, c),
			Tax: mu.format(inv.Totals.Tax, c), Total: mu.format(inv.Totals.Total, c),
		},
		Bank: BankDetails{
			BankName: b.BankName, Branch: b.Branch, BranchCode: b.BranchCode, AccountName: b.AccountName,
			AccountNumber: b.AccountNumber, AccountType: b.AccountType, SwiftBic: b.SwiftBIC, Iban: b.IBAN,
		},
		ScheduleId: inv.ScheduleID, Overdue: inv.IsOverdue(today), IssuedAt: timePtr(inv.IssuedAt),
		SentAt: timePtr(inv.SentAt), PaidAt: timePtr(inv.PaidAt), VoidedAt: timePtr(inv.VoidedAt),
		VoidReason: inv.VoidReason, CreatedAt: inv.CreatedAt.UTC(), UpdatedAt: inv.UpdatedAt.UTC(), Version: inv.Version,
	}
}

func toAPIEmail(m domain.EmailMessage) EmailMessage {
	to, cc := m.To, m.CC
	if to == nil {
		to = []string{}
	}
	if cc == nil {
		cc = []string{}
	}
	return EmailMessage{
		Id: m.ID, InvoiceId: m.InvoiceID, To: to, Cc: cc, Subject: m.Subject, Body: m.Body,
		Status: EmailStatus(m.Status), Attempts: m.Attempts, SendAt: m.SendAt.UTC(), NextAttemptAt: m.NextAttemptAt.UTC(),
		LastError: m.LastError, SentAt: timePtr(m.SentAt), CreatedAt: m.CreatedAt.UTC(),
	}
}

// ---- schedules ----

func toAPIRecurrence(r domain.Recurrence) Recurrence {
	var end *openapi_types.Date
	if !r.EndDate.IsZero() {
		end = toAPIDatePtr(r.EndDate)
	}
	return Recurrence{
		Frequency: RecurrenceFrequency(r.Frequency), Interval: r.Interval, DayOfMonth: r.DayOfMonth,
		Weekday: int(r.Weekday), SendTime: fmt.Sprintf("%02d:%02d", r.SendHour, r.SendMinute),
		TimeZone: r.TimeZone, StartDate: toAPIDate(r.StartDate), EndDate: end,
	}
}

func fromAPIRecurrence(r Recurrence) (domain.Recurrence, error) {
	hour, minute := 9, 0
	if r.SendTime != "" {
		t, err := time.Parse("15:04", r.SendTime)
		if err != nil {
			return domain.Recurrence{}, &domain.ValidationError{Field: "recurrence.sendTime", Reason: "must be HH:MM"}
		}
		hour, minute = t.Hour(), t.Minute()
	}
	return domain.Recurrence{
		Frequency: domain.Frequency(r.Frequency), Interval: r.Interval, DayOfMonth: r.DayOfMonth,
		Weekday: time.Weekday(r.Weekday), SendHour: hour, SendMinute: minute, TimeZone: r.TimeZone,
		StartDate: fromAPIDate(r.StartDate), EndDate: fromAPIDatePtr(r.EndDate),
	}, nil
}

func toAPISchedule(s *domain.Schedule, currency domain.CurrencyCode, mu minorUnits) Schedule {
	lines := make([]ScheduleLine, len(s.Lines))
	for i, l := range s.Lines {
		var price *string
		if l.UnitPrice != nil {
			v := mu.format(*l.UnitPrice, currency)
			price = &v
		}
		lines[i] = ScheduleLine{
			ProductId: l.ProductID, Description: l.Description, Quantity: l.Quantity.String(), UnitPrice: price,
			DiscountPercent: l.Discount.String(),
		}
	}
	return Schedule{
		Id: s.ID, CustomerId: s.CustomerID, Name: s.Name, Status: ScheduleStatus(s.Status),
		Recurrence: toAPIRecurrence(s.Recurrence), Lines: lines, Header: toAPIHeader(s.Header), Notes: s.Notes,
		TaxPercent: s.TaxRate.String(), AutoSend: s.AutoSend, EmailSubject: s.EmailSubject, EmailBody: s.EmailBody,
		NextRunAt: timePtr(s.NextRunAt), LastRunAt: timePtr(s.LastRunAt), FailureCount: s.FailureCount,
		LastError: s.LastError, Version: s.Version,
	}
}

func fromAPISchedule(id string, s Schedule, currency domain.CurrencyCode, mu minorUnits) (domain.Schedule, error) {
	rec, err := fromAPIRecurrence(s.Recurrence)
	if err != nil {
		return domain.Schedule{}, err
	}
	tax, err := percent("taxPercent", s.TaxPercent)
	if err != nil {
		return domain.Schedule{}, err
	}
	out := domain.Schedule{
		ID: id, CustomerID: s.CustomerId, Name: s.Name, Recurrence: rec, Header: fromAPIHeader(s.Header),
		Notes: s.Notes, TaxRate: tax, AutoSend: s.AutoSend, EmailSubject: s.EmailSubject, EmailBody: s.EmailBody,
		Version: s.Version,
	}
	for _, l := range s.Lines {
		q, err := quantity("lines.quantity", l.Quantity)
		if err != nil {
			return domain.Schedule{}, err
		}
		disc, err := percent("lines.discountPercent", l.DiscountPercent)
		if err != nil {
			return domain.Schedule{}, err
		}
		sl := domain.ScheduleLine{ProductID: l.ProductId, Description: l.Description, Quantity: q, Discount: disc}
		if l.UnitPrice != nil {
			v, err := mu.parse("lines.unitPrice", *l.UnitPrice, currency)
			if err != nil {
				return domain.Schedule{}, err
			}
			sl.UnitPrice = &v
		}
		out.Lines = append(out.Lines, sl)
	}
	return out, nil
}
