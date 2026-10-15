package domain

import (
	"sort"
	"time"
)

// StatementLine is one unpaid invoice on a statement.
type StatementLine struct {
	InvoiceID   string
	Number      string
	IssueDate   Date
	DueDate     Date
	Amount      int64
	DaysOverdue int
	Status      InvoiceStatus
}

// Aging buckets outstanding amounts by days past the due date.
type Aging struct {
	Current    int64 // not yet due
	Days1to30  int64
	Days31to60 int64
	Days61to90 int64
	Over90     int64
}

// Statement is a customer's statement of account: every unpaid invoice in the customer's
// currency, with aging, as of a date.
type Statement struct {
	Customer Customer
	Currency CurrencyCode
	AsOf     Date
	Lines    []StatementLine
	Aging    Aging
	Total    int64
}

// NewStatement builds a statement for c from its invoices (any order, any status).
func NewStatement(c Customer, invoices []Invoice, asOf Date) Statement {
	st := Statement{Customer: c, Currency: c.Currency, AsOf: asOf}
	for i := range invoices {
		inv := &invoices[i]
		if inv.CustomerID != c.ID || !inv.IsOpen() || inv.Currency != c.Currency {
			continue
		}
		days := 0
		if inv.DueDate.Before(asOf) {
			days = int(asOf.Time(time.UTC).Sub(inv.DueDate.Time(time.UTC)).Hours() / 24)
		}
		amt := inv.Totals.Total
		st.Lines = append(st.Lines, StatementLine{
			InvoiceID: inv.ID, Number: inv.Number, IssueDate: inv.IssueDate, DueDate: inv.DueDate,
			Amount: amt, DaysOverdue: days, Status: inv.Status,
		})
		st.Total += amt
		switch {
		case days == 0:
			st.Aging.Current += amt
		case days <= 30:
			st.Aging.Days1to30 += amt
		case days <= 60:
			st.Aging.Days31to60 += amt
		case days <= 90:
			st.Aging.Days61to90 += amt
		default:
			st.Aging.Over90 += amt
		}
	}
	sort.Slice(st.Lines, func(i, j int) bool {
		if st.Lines[i].IssueDate != st.Lines[j].IssueDate {
			return st.Lines[i].IssueDate.Before(st.Lines[j].IssueDate)
		}
		return st.Lines[i].Number < st.Lines[j].Number
	})
	return st
}

// Default statement e-mail templates.
const (
	DefaultStatementSubject = "Statement of account from {{business_name}}"
	DefaultStatementBody    = "Dear {{customer_name}},\n\nPlease find attached your statement of account. " +
		"The outstanding balance is {{total}}.\n\nKind regards,\n{{business_name}}"
)
