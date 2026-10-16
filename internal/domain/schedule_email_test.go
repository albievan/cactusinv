package domain

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestDefaultScheduleEmails(t *testing.T) {
	d := DefaultScheduleEmails()
	invoiceData := TemplateData{InvoiceNumber: "CD/1195/26", CustomerName: "Acme", Attention: "Jane", Total: "R 3,251.16", DueDate: "7 September 2026", IssueDate: "21 August 2026", BusinessName: "Example Software"}
	// A statement fills no invoice number or due date.
	statementData := TemplateData{CustomerName: "Acme", Attention: "Jane", Total: "R 3,251.16", IssueDate: "16 October 2026", BusinessName: "Example Software"}
	placeholder := regexp.MustCompile(`\{\{[a-z_]+\}\}`)
	tests := []struct {
		name string
		tmpl EmailTemplate
		data TemplateData
		want []string // text the rendered body must contain
	}{
		{"recurring invoice", d.Invoice, invoiceData, []string{"invoice CD/1195/26 dated 21 August 2026 for R 3,251.16, due 7 September 2026", "Thank you for your business."}},
		{"unsent invoices", d.Unsent, invoiceData, []string{"invoice CD/1195/26", "R 3,251.16"}},
		{"outstanding invoices", d.Outstanding, invoiceData, []string{"courteous reminder that invoice CD/1195/26 for R 3,251.16, due 7 September 2026", "If you have already paid"}},
		{"statement", d.Statement, statementData, []string{"statement of account as at 16 October 2026", "The balance outstanding is R 3,251.16"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.tmpl.Subject == "" || tc.tmpl.Body == "" || len(tc.tmpl.Subject) > 300 || len(tc.tmpl.Body) > 10000 {
				t.Fatalf("template = %+v", tc.tmpl)
			}
			for _, p := range placeholder.FindAllString(tc.tmpl.Subject+tc.tmpl.Body, -1) {
				if !slices.Contains(tc.tmpl.Placeholders, p) {
					t.Errorf("%s is not filled for this type", p)
				}
			}
			subject, body := SubjectLine(tc.tmpl.Subject, tc.data), RenderTemplate(tc.tmpl.Body, tc.data)
			if strings.Contains(subject+body, "{{") || strings.Contains(subject, "\n") {
				t.Fatalf("unrendered: %q / %q", subject, body)
			}
			if !strings.HasPrefix(body, "Dear Acme,") || !strings.HasSuffix(body, "Kind regards,\nExample Software") || !strings.Contains(subject, "Example Software") {
				t.Fatalf("greeting, sign-off or sender missing:\n%s\n%s", subject, body)
			}
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("body lacks %q:\n%s", w, body)
				}
			}
		})
	}
	// Every listed placeholder is one the renderer replaces.
	for _, p := range append(d.Invoice.Placeholders, d.Statement.Placeholders...) {
		if RenderTemplate(p, invoiceData) == p {
			t.Errorf("%s is not substituted", p)
		}
	}
	if d.Statement.Subject != "Statement of account from {{business_name}} as at {{date}}" || slices.Contains(d.Statement.Placeholders, "{{invoice_number}}") {
		t.Fatalf("statement = %+v", d.Statement)
	}
	// Each call returns its own lists.
	d.Invoice.Placeholders[0] = "x"
	if DefaultScheduleEmails().Invoice.Placeholders[0] == "x" {
		t.Fatal("shared placeholder list")
	}
}

func TestRenderTemplateDate(t *testing.T) {
	if got := RenderTemplate("as at {{date}} ({{issue_date}})", TemplateData{IssueDate: "16 October 2026"}); got != "as at 16 October 2026 (16 October 2026)" {
		t.Fatalf("got %q", got)
	}
}

func TestScheduleEmailsFor(t *testing.T) {
	base := DefaultScheduleEmails()
	tests := []struct {
		name          string
		b             BusinessProfile
		subject, body string
	}{
		{"nothing set", BusinessProfile{}, base.Invoice.Subject, base.Invoice.Body},
		{"the built-in business defaults count as not set", BusinessProfile{EmailSubject: DefaultEmailSubject, EmailBody: DefaultEmailBody}, base.Invoice.Subject, base.Invoice.Body},
		{"the business's own texts", BusinessProfile{EmailSubject: "Your {{invoice_number}}", EmailBody: "Hi {{customer_name}}"}, "Your {{invoice_number}}", "Hi {{customer_name}}"},
		{"only a subject of its own", BusinessProfile{EmailSubject: "Your {{invoice_number}}", EmailBody: DefaultEmailBody}, "Your {{invoice_number}}", base.Invoice.Body},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := ScheduleEmailsFor(tc.b)
			if d.Invoice.Subject != tc.subject || d.Invoice.Body != tc.body {
				t.Fatalf("invoice = %q / %q", d.Invoice.Subject, d.Invoice.Body)
			}
			if d.Unsent.Subject != base.Unsent.Subject || d.Unsent.Body != base.Unsent.Body || d.Statement.Subject != base.Statement.Subject || d.Outstanding.Body != base.Outstanding.Body {
				t.Fatal("other types must keep their templates")
			}
		})
	}
}
