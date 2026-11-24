package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/app"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// errFault is the database failure the sweep injects.
var errFault = errors.New("injected store failure")

// TestEveryStoreFailureIsReported walks use cases through a database failure at every step
// they take (fakes.Store.FailAt 1, 2, 3, …) until one runs past them all. Each failure must
// come back as an error wrapping the injected one: none is lost or turned into another.
// A use case that knowingly tolerates a failure (best effort) is listed in tolerant.
func TestEveryStoreFailureIsReported(t *testing.T) {
	type prep func(t *testing.T, h *harness) func() error
	quote := func(t *testing.T, h *harness) *domain.Quote {
		t.Helper()
		return must(h.svc.CreateQuote(h.ctx, h.quoteInput()))
	}
	issued := func(t *testing.T, h *harness) *domain.Invoice {
		t.Helper()
		return must(h.svc.IssueInvoice(h.ctx, must(h.svc.CreateInvoice(h.ctx, h.referenceInput())).ID, domain.Date{}))
	}
	schedule := func(t *testing.T, h *harness) *domain.Schedule {
		t.Helper()
		return must(h.svc.CreateSchedule(h.ctx, domain.Schedule{
			CustomerID: h.customer.ID, Name: "Monthly",
			Recurrence: domain.Recurrence{Frequency: domain.FrequencyMonthly, DayOfMonth: 1, SendHour: 9, StartDate: domain.NewDate(2026, 9, 1)},
			Lines:      []domain.ScheduleLine{{ProductID: h.usersP.ID, Quantity: 1000}},
		})).Schedule
	}
	adhoc := func(t *testing.T, h *harness) domain.AdhocCustomer {
		t.Helper()
		return must(h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "Prospect", Email: "p@prospect.example", Currency: "ZAR"}))
	}
	cases := map[string]prep{
		// Quotes (M34, M41).
		"CreateQuote": func(t *testing.T, h *harness) func() error {
			return func() error { _, err := h.svc.CreateQuote(h.ctx, h.quoteInput()); return err }
		},
		"UpdateQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error { _, err := h.svc.UpdateQuote(h.ctx, q.ID, q.Version, h.quoteInput()); return err }
		},
		"SendQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error {
				_, err := h.svc.SendQuote(h.ctx, q.ID, app.SendInput{LinkBase: "https://inv.example.com"})
				return err
			}
		},
		"AcceptQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error { _, err := h.svc.AcceptQuote(h.ctx, q.ID); return err }
		},
		"ConvertQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			must(h.svc.AcceptQuote(h.ctx, q.ID))
			return func() error { _, err := h.svc.ConvertQuote(h.ctx, q.ID); return err }
		},
		"RenewQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			must(h.svc.DeclineQuote(h.ctx, q.ID, "no"))
			return func() error { _, err := h.svc.RenewQuote(h.ctx, q.ID); return err }
		},
		"DeleteQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error { return h.svc.DeleteQuote(h.ctx, q.ID) }
		},
		"GetQuote": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error { _, err := h.svc.GetQuote(h.ctx, q.ID); return err }
		},
		"QuotePDF": func(t *testing.T, h *harness) func() error {
			q := quote(t, h)
			return func() error { _, _, err := h.svc.QuotePDF(h.ctx, q.ID); return err }
		},
		"ListQuotes": func(t *testing.T, h *harness) func() error {
			quote(t, h)
			return func() error { _, _, err := h.svc.ListQuotes(h.ctx, ports.QuoteFilter{}); return err }
		},
		"QuoteSettings": func(t *testing.T, h *harness) func() error {
			return func() error {
				if _, err := h.svc.UpdateQuoteSettings(h.ctx, domain.DefaultQuoteSettings(), 10); err != nil {
					return err
				}
				_, err := h.svc.GetQuoteSettings(h.ctx)
				return err
			}
		},
		"RespondToQuote": func(t *testing.T, h *harness) func() error {
			token := tokenIn(t, must(h.svc.SendQuote(h.ctx, quote(t, h).ID, app.SendInput{LinkBase: "https://inv.example.com"})).Body)
			return func() error {
				_, err := h.svc.RespondToQuote(h.ctx, token, domain.QuoteAnswerDecline, "no")
				return err
			}
		},
		// Ad-hoc customers.
		"CreateAdhocCustomer": func(t *testing.T, h *harness) func() error {
			return func() error {
				_, err := h.svc.CreateAdhocCustomer(h.ctx, domain.AdhocCustomer{Name: "P", Currency: "ZAR"})
				return err
			}
		},
		"UpdateAdhocCustomer": func(t *testing.T, h *harness) func() error {
			a := adhoc(t, h)
			a.Phone = "011"
			return func() error { _, err := h.svc.UpdateAdhocCustomer(h.ctx, a); return err }
		},
		"ConvertAdhocCustomer": func(t *testing.T, h *harness) func() error {
			a := adhoc(t, h)
			must(h.svc.CreateQuote(h.ctx, app.QuoteInput{AdhocCustomerID: a.ID, Lines: []app.LineInput{{ProductID: h.usersP.ID, Quantity: 1000}}}))
			return func() error {
				_, err := h.svc.ConvertAdhocCustomer(h.ctx, a.ID, domain.CustomerDetails{Code: "PR", Email: "p@prospect.example"})
				return err
			}
		},
		"DeleteAdhocCustomer": func(t *testing.T, h *harness) func() error {
			a := adhoc(t, h)
			return func() error { return h.svc.DeleteAdhocCustomer(h.ctx, a.ID) }
		},
		"ListAdhocCustomers": func(t *testing.T, h *harness) func() error {
			adhoc(t, h)
			return func() error { _, _, err := h.svc.ListAdhocCustomers(h.ctx, ports.AdhocCustomerFilter{}); return err }
		},
		// Invoices (and the customer's "I have paid", M42).
		"SendInvoice": func(t *testing.T, h *harness) func() error {
			inv := must(h.svc.CreateInvoice(h.ctx, h.referenceInput()))
			return func() error {
				_, err := h.svc.SendInvoice(h.ctx, inv.ID, app.SendInput{LinkBase: "https://inv.example.com"})
				return err
			}
		},
		"MarkInvoicePaid": func(t *testing.T, h *harness) func() error {
			inv := issued(t, h)
			return func() error { _, err := h.svc.MarkInvoicePaid(h.ctx, inv.ID); return err }
		},
		"RecordPayment": func(t *testing.T, h *harness) func() error {
			inv := issued(t, h)
			return func() error { _, err := h.svc.RecordPayment(h.ctx, inv.ID, app.PaymentInput{Amount: 100}); return err }
		},
		"RemovePayment": func(t *testing.T, h *harness) func() error {
			inv := must(h.svc.RecordPayment(h.ctx, issued(t, h).ID, app.PaymentInput{Amount: 100}))
			p := must(h.svc.ListPayments(h.ctx, inv.ID))[0]
			return func() error { _, err := h.svc.RemovePayment(h.ctx, inv.ID, p.ID); return err }
		},
		"UpdateInvoiceNotes": func(t *testing.T, h *harness) func() error {
			inv := issued(t, h)
			return func() error {
				_, err := h.svc.UpdateInvoiceNotes(h.ctx, inv.ID, inv.Version, "Paid in two parts.")
				return err
			}
		},
		"MarkInvoicePaidByCustomer": func(t *testing.T, h *harness) func() error {
			token := paidToken(t, must(h.svc.SendInvoice(h.ctx, issued(t, h).ID, app.SendInput{LinkBase: "https://inv.example.com"})).Body)
			return func() error { _, err := h.svc.MarkInvoicePaidByCustomer(h.ctx, token, "EFT"); return err }
		},
		// Schedules' upcoming invoices and the dashboard.
		"ListUpcomingInvoices": func(t *testing.T, h *harness) func() error {
			schedule(t, h)
			return func() error { _, err := h.svc.ListUpcomingInvoices(h.ctx); return err }
		},
		"ScheduleSamplePDF": func(t *testing.T, h *harness) func() error {
			sc := schedule(t, h)
			return func() error { _, _, err := h.svc.ScheduleSamplePDF(h.ctx, sc.ID); return err }
		},
		"GetDashboard": func(t *testing.T, h *harness) func() error {
			quote(t, h)
			issued(t, h)
			schedule(t, h)
			return func() error { _, err := h.svc.GetDashboard(h.ctx); return err }
		},
		// Your account, forgotten passwords, users (M43, M44).
		"UpdateProfile": func(t *testing.T, h *harness) func() error {
			u := must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
			return func() error {
				_, err := h.svc.UpdateProfile(h.ctx, u.ID, domain.Profile{FirstName: "Albie"})
				return err
			}
		},
		"RequestPasswordReset": func(t *testing.T, h *harness) func() error {
			must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
			return func() error { return h.svc.RequestPasswordReset(h.ctx, "albie@example.com", "https://inv.example.com") }
		},
		"ResetPassword": func(t *testing.T, h *harness) func() error {
			must(h.svc.CreateUser(h.ctx, "albie@example.com", "correct horse battery"))
			must0(t, h.svc.RequestPasswordReset(h.ctx, "albie@example.com", "https://inv.example.com"))
			list, _ := must3(h.svc.ListEmails(h.ctx, ports.EmailFilter{}))
			token := resetLinkRe.FindStringSubmatch(list[0].Body)[1]
			return func() error {
				if err := h.svc.CheckPasswordReset(h.ctx, token); err != nil {
					return err
				}
				return h.svc.ResetPassword(h.ctx, token, "a brand new long password")
			}
		},
		"UpdateUser": func(t *testing.T, h *harness) func() error {
			ctx, _, other := h.users()
			return func() error {
				_, err := h.svc.UpdateUser(ctx, other.ID, app.UserEdit{Login: "thandi@example.com", Profile: domain.Profile{FirstName: "Thandi"}}, adminPW)
				return err
			}
		},
		"DeleteUser": func(t *testing.T, h *harness) func() error {
			ctx, _, other := h.users()
			return func() error { return h.svc.DeleteUser(ctx, other.ID, adminPW) }
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			for n := 1; ; n++ {
				if n > 300 {
					t.Fatal("never ran past the injected failures")
				}
				h := newHarness(t).seed()
				act := prepare(t, h)
				h.clock.Advance(time.Minute) // past the reset e-mail interval and the like
				h.store.FailAt(n, errFault)
				err := act()
				pending := h.store.FailPending()
				h.store.FailAt(0, nil)
				if pending { // ran to the end before call n: done
					t.Logf("%d failure points", n-1)
					if err != nil {
						t.Fatalf("no failure injected, yet: %v", err)
					}
					if n < 2 {
						t.Fatal("the use case made no database call")
					}
					return
				}
				if err != nil && !errors.Is(err, errFault) {
					t.Errorf("failure at call %d came back as another error: %v", n, err)
				}
				if err == nil && !tolerant[name] {
					t.Errorf("failure at call %d was lost: the use case reported success", n)
				}
			}
		})
	}
}

// tolerant lists use cases that deliberately carry on past some failures (best effort,
// e.g. an audit entry about a refusal); their other failures are still checked.
var tolerant = map[string]bool{}
