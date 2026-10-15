package app

import (
	"context"
	"sort"
	"strings"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// GetStatement builds a customer's statement of account as of today.
func (s *Service) GetStatement(ctx context.Context, customerID string) (domain.Statement, error) {
	doc, err := s.statementDoc(ctx, s.tx.Repos(), customerID)
	return doc.Statement, wrap("app.GetStatement", err)
}

// StatementPDF renders a customer's statement; the content is current at render time.
func (s *Service) StatementPDF(ctx context.Context, customerID string) ([]byte, string, error) {
	doc, err := s.statementDoc(ctx, s.tx.Repos(), customerID)
	if err != nil {
		return nil, "", wrap("app.StatementPDF", err)
	}
	pdf, err := s.renderer.RenderStatement(ctx, doc)
	if err != nil {
		return nil, "", wrap("app.StatementPDF", err)
	}
	if err := s.auditNow(ctx, "customer.statement_viewed", "customer", customerID, ""); err != nil {
		return nil, "", wrap("app.StatementPDF", err)
	}
	return pdf, StatementFilename(doc.Statement), nil
}

// StatementFilename is e.g. "Statement_CD_2026-10-15.pdf".
func StatementFilename(st domain.Statement) string {
	return "Statement_" + st.Customer.Code + "_" + st.AsOf.String() + ".pdf"
}

func (s *Service) statementDoc(ctx context.Context, r ports.Repos, customerID string) (ports.StatementDocument, error) {
	c, err := r.Customers.Get(ctx, customerID)
	if err != nil {
		return ports.StatementDocument{}, err
	}
	b, err := business(ctx, r)
	if err != nil {
		return ports.StatementDocument{}, err
	}
	cur, err := r.Currencies.Get(ctx, c.Currency)
	if err != nil {
		return ports.StatementDocument{}, err
	}
	var all []domain.Invoice
	for offset := 0; ; offset += 200 {
		page, total, err := r.Invoices.List(ctx, ports.InvoiceFilter{CustomerID: c.ID, Page: ports.Page{Limit: 200, Offset: offset}})
		if err != nil {
			return ports.StatementDocument{}, err
		}
		all = append(all, page...)
		if offset+len(page) >= total || len(page) == 0 {
			break
		}
	}
	doc := ports.StatementDocument{
		Statement: domain.NewStatement(c, all, s.today(b)), Business: b, Currency: cur,
		Disclaimer: domain.Disclaimer(c, b),
	}
	if accounts, err := r.Banks.List(ctx, c.Currency); err == nil {
		if acct, err := domain.ResolveBankAccount(c, accounts); err == nil {
			doc.Bank = acct.Details()
		}
	}
	return doc, nil
}

// SendStatement queues a statement e-mail for a customer (now or at in.SendAt). The PDF is
// rendered when the e-mail is sent, so it is current on delivery.
func (s *Service) SendStatement(ctx context.Context, customerID string, in SendInput) (*domain.EmailMessage, error) {
	var out *domain.EmailMessage
	err := s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		if in.IdempotencyKey != "" {
			if m, err := r.Emails.GetByIdempotencyKey(ctx, in.IdempotencyKey); err == nil {
				out = m
				return nil
			}
		}
		m, err := s.enqueueStatement(ctx, r, customerID, in)
		out = m
		return err
	})
	return out, wrap("app.SendStatement", err)
}

func (s *Service) enqueueStatement(ctx context.Context, r ports.Repos, customerID string, in SendInput) (*domain.EmailMessage, error) {
	doc, err := s.statementDoc(ctx, r, customerID)
	if err != nil {
		return nil, err
	}
	c := doc.Statement.Customer
	to, cc := in.To, in.CC
	if len(to) == 0 {
		to, cc = []string{c.Email}, c.CC
	}
	sym := doc.Currency.Symbol
	if sym == "" {
		sym = string(doc.Currency.Code)
	}
	data := domain.TemplateData{
		CustomerName: c.Name, Attention: c.Attention, BusinessName: doc.Business.Name,
		Total:     sym + " " + domain.FormatGrouped(doc.Statement.Total, doc.Currency.MinorUnits, ","),
		IssueDate: doc.Statement.AsOf.Long(),
	}
	subject, body := strings.TrimSpace(in.Subject), in.Body
	if subject == "" {
		subject = domain.DefaultStatementSubject
	}
	if body == "" {
		body = domain.DefaultStatementBody
	}
	m, err := domain.NewEmailMessage(s.ids.NewID(), "", to, cc, domain.SubjectLine(subject, data),
		domain.RenderTemplate(body, data), in.SendAt, s.now(), in.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	m.Kind, m.CustomerID = domain.EmailKindStatement, c.ID
	if err := r.Emails.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, s.audit(ctx, r, "customer.statement_queued", "customer", c.ID, "to "+strings.Join(to, ", ")+", balance "+data.Total)
}

func (s *Service) deliverStatement(ctx context.Context, m *domain.EmailMessage) error {
	doc, err := s.statementDoc(ctx, s.tx.Repos(), m.CustomerID)
	if err != nil {
		return err
	}
	pdf, err := s.renderer.RenderStatement(ctx, doc)
	if err != nil {
		return err
	}
	if err := s.mailer.Send(ctx, ports.Mail{
		MessageID: m.ID, To: m.To, CC: m.CC, ReplyTo: doc.Business.Email, Subject: m.Subject, Body: m.Body,
		Attachments: []ports.Attachment{{Filename: StatementFilename(doc.Statement), ContentType: "application/pdf", Data: pdf}},
	}); err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context, r ports.Repos) error {
		cur, err := r.Emails.Get(ctx, m.ID)
		if err != nil {
			return err
		}
		cur.Succeeded(s.now())
		if err := r.Emails.Update(ctx, cur); err != nil {
			return err
		}
		return s.audit(ctx, r, "customer.statement_emailed", "customer", m.CustomerID, "to "+strings.Join(m.To, ", "))
	})
}

// StatementSummary is one customer's statement position for the Statements overview.
type StatementSummary struct {
	Statement domain.Statement
	// LastEmail is the customer's most recent statement e-mail (nil if never).
	LastEmail *domain.EmailMessage
}

// ListStatements builds the statement of every active customer as of today, largest
// balance first; customers with nothing owed follow, by name.
func (s *Service) ListStatements(ctx context.Context) ([]StatementSummary, error) {
	r := s.tx.Repos()
	b, err := business(ctx, r)
	if err != nil {
		return nil, wrap("app.ListStatements", err)
	}
	today := s.today(b)
	var customers []domain.Customer
	for offset := 0; ; offset += 200 {
		page, total, err := r.Customers.List(ctx, ports.CustomerFilter{Page: ports.Page{Limit: 200, Offset: offset}})
		if err != nil {
			return nil, wrap("app.ListStatements", err)
		}
		customers = append(customers, page...)
		if offset+len(page) >= total || len(page) == 0 {
			break
		}
	}
	open := map[string][]domain.Invoice{}
	for _, st := range []domain.InvoiceStatus{domain.InvoiceIssued, domain.InvoiceSent, domain.InvoiceOverdue} {
		for offset := 0; ; offset += 200 {
			page, total, err := r.Invoices.List(ctx, ports.InvoiceFilter{Status: st, Page: ports.Page{Limit: 200, Offset: offset}})
			if err != nil {
				return nil, wrap("app.ListStatements", err)
			}
			for _, inv := range page {
				open[inv.CustomerID] = append(open[inv.CustomerID], inv)
			}
			if offset+len(page) >= total || len(page) == 0 {
				break
			}
		}
	}
	out := make([]StatementSummary, 0, len(customers))
	for _, c := range customers {
		sum := StatementSummary{Statement: domain.NewStatement(c, open[c.ID], today)}
		last, _, err := r.Emails.List(ctx, ports.EmailFilter{CustomerID: c.ID, Page: ports.Page{Limit: 1}})
		if err != nil {
			return nil, wrap("app.ListStatements", err)
		}
		if len(last) > 0 {
			sum.LastEmail = &last[0]
		}
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Statement, out[j].Statement
		if (a.Total > 0) != (b.Total > 0) {
			return a.Total > 0
		}
		if a.Currency != b.Currency && a.Total > 0 {
			return a.Currency < b.Currency
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return strings.ToLower(a.Customer.Name) < strings.ToLower(b.Customer.Name)
	})
	return out, nil
}

// BulkStatementResult reports a bulk statement send.
type BulkStatementResult struct {
	Queued  []string          // customer ids
	Skipped map[string]string // customer id → reason
}

// SendStatements queues a statement e-mail for each customer, skipping customers with
// nothing owed (unless includeZero) or without an e-mail address. Each send is its own
// transaction, so one failure does not stop the rest.
func (s *Service) SendStatements(ctx context.Context, customerIDs []string, includeZero bool) (BulkStatementResult, error) {
	res := BulkStatementResult{Skipped: map[string]string{}}
	if len(customerIDs) == 0 {
		return res, wrap("app.SendStatements", domain.ErrValidation)
	}
	for _, id := range customerIDs {
		st, err := s.GetStatement(ctx, id)
		switch {
		case err != nil:
			res.Skipped[id] = "customer not found"
			continue
		case st.Customer.Email == "":
			res.Skipped[id] = "no e-mail address"
			continue
		case st.Total == 0 && !includeZero:
			res.Skipped[id] = "nothing owed"
			continue
		}
		if _, err := s.SendStatement(ctx, id, SendInput{}); err != nil {
			res.Skipped[id] = "could not queue the e-mail"
			continue
		}
		res.Queued = append(res.Queued, id)
	}
	return res, nil
}
