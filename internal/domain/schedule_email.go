package domain

import "strings"

// EmailTemplate is an e-mail subject and body with {{placeholders}} (see RenderTemplate),
// and the placeholders the server fills for the kind of e-mail it is used for.
type EmailTemplate struct {
	Subject      string
	Body         string
	Placeholders []string
}

// Placeholders filled for each kind of e-mail: an invoice e-mail knows its invoice; a
// statement knows its customer, balance ({{total}}) and as-at date only.
func invoicePlaceholders() []string {
	return []string{"{{customer_name}}", "{{attention}}", "{{invoice_number}}", "{{total}}", "{{due_date}}", "{{date}}", "{{issue_date}}", "{{business_name}}"}
}

func statementPlaceholders() []string {
	return []string{"{{customer_name}}", "{{attention}}", "{{total}}", "{{date}}", "{{business_name}}"}
}

// ScheduleEmailDefaults are the suggested e-mail texts for a new schedule of each type.
type ScheduleEmailDefaults struct {
	Invoice     EmailTemplate // recurring invoice: raise, then e-mail it
	Unsent      EmailTemplate // e-mail issued invoices never e-mailed
	Outstanding EmailTemplate // e-mail every unpaid invoice again
	Statement   EmailTemplate // e-mail one statement of account
}

// DefaultScheduleEmails returns the suggested e-mail subject and body for each schedule
// type: concise, polite and signed with the business name, using only the placeholders
// the server fills for that type.
func DefaultScheduleEmails() ScheduleEmailDefaults {
	const sign = "\n\nKind regards,\n{{business_name}}"
	const questions = "\n\nIf you have any questions, simply reply to this e-mail."
	invoiceBody := "Dear {{customer_name}},\n\nPlease find attached invoice {{invoice_number}} dated {{date}} for {{total}}, due {{due_date}}." +
		"\n\nPayment details are on the invoice; please use {{invoice_number}} as your payment reference." +
		questions + "\n\nThank you for your business." + sign
	return ScheduleEmailDefaults{
		Invoice: EmailTemplate{
			Subject: "Invoice {{invoice_number}} from {{business_name}}", Body: invoiceBody, Placeholders: invoicePlaceholders(),
		},
		Unsent: EmailTemplate{
			Subject: "Invoice {{invoice_number}} from {{business_name}}", Body: invoiceBody, Placeholders: invoicePlaceholders(),
		},
		Outstanding: EmailTemplate{
			Subject: "Reminder: invoice {{invoice_number}} from {{business_name}}",
			Body: "Dear {{customer_name}},\n\nThis is a courteous reminder that invoice {{invoice_number}} for {{total}}, due {{due_date}}, is still open on our records. " +
				"A copy is attached for your convenience." +
				"\n\nIf you have already paid, thank you, and please disregard this e-mail. Otherwise we would appreciate payment by the due date, or as soon as possible if it has passed." +
				questions + sign,
			Placeholders: invoicePlaceholders(),
		},
		Statement: EmailTemplate{
			Subject: "Statement of account from {{business_name}} as at {{date}}",
			Body: "Dear {{customer_name}},\n\nPlease find attached your statement of account as at {{date}}. The balance outstanding is {{total}}." +
				"\n\nIf you have already paid, thank you, and please disregard this e-mail. If anything on the statement looks wrong, simply reply and we will look into it." +
				sign,
			Placeholders: statementPlaceholders(),
		},
	}
}

// ScheduleEmailsFor returns the suggested texts with the business's own invoice e-mail
// subject and body (Settings → Business) for the recurring-invoice type, each where the
// business has set one other than the built-in DefaultEmailSubject/DefaultEmailBody. The
// other types keep their templates.
func ScheduleEmailsFor(b BusinessProfile) ScheduleEmailDefaults {
	d := DefaultScheduleEmails()
	if v := strings.TrimSpace(b.EmailSubject); v != "" && v != DefaultEmailSubject {
		d.Invoice.Subject = b.EmailSubject
	}
	if v := strings.TrimSpace(b.EmailBody); v != "" && v != DefaultEmailBody {
		d.Invoice.Body = b.EmailBody
	}
	return d
}
