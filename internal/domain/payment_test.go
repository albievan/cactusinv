package domain

import (
	"errors"
	"testing"
	"time"
)

func openInvoice(total int64) *Invoice {
	return &Invoice{ID: "inv", Status: InvoiceSent, Totals: Totals{Total: total}, SentAt: time.Unix(1, 0)}
}

func TestRecordPayment(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	on := NewDate(2026, 11, 23)
	inv := openInvoice(10000)

	for _, bad := range []Payment{{Amount: 0, ReceivedOn: on}, {Amount: -5, ReceivedOn: on}, {Amount: 10001, ReceivedOn: on}, {Amount: 100}} {
		if err := inv.RecordPayment(&bad, now); !errors.Is(err, ErrValidation) {
			t.Fatalf("%+v: want validation error, got %v", bad, err)
		}
	}
	long := Payment{Amount: 1, ReceivedOn: on, Reference: string(make([]rune, MaxPaymentReferenceLength+1))}
	if err := inv.RecordPayment(&long, now); !errors.Is(err, ErrValidation) {
		t.Fatalf("long reference: %v", err)
	}

	p := Payment{Amount: 4000, ReceivedOn: on, Reference: "  EFT 1  "}
	if err := inv.RecordPayment(&p, now); err != nil {
		t.Fatal(err)
	}
	if inv.AmountPaid != 4000 || inv.Balance() != 6000 || inv.Status != InvoiceSent || p.Reference != "EFT 1" || p.InvoiceID != "inv" {
		t.Fatalf("part-payment: %+v %+v", inv, p)
	}
	if err := inv.Void("x", now); !errors.Is(err, ErrValidation) {
		t.Fatalf("void with payments: %v", err)
	}

	rest, err := inv.MarkPaid(on, now)
	if err != nil || rest.Amount != 6000 || inv.Status != InvoicePaid || inv.Balance() != 0 || !inv.PaidAt.Equal(now) {
		t.Fatalf("mark paid: %v %+v %+v", err, rest, inv)
	}
	more := Payment{Amount: 1, ReceivedOn: on}
	if err := inv.RecordPayment(&more, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("paid invoice takes no payment: %v", err)
	}

	// Removing a payment opens it again, as sent.
	if err := inv.RemovePayment(rest, now); err != nil || inv.Status != InvoiceSent || inv.Balance() != 6000 || !inv.PaidAt.IsZero() {
		t.Fatalf("remove: %v %+v", err, inv)
	}
	if err := inv.RemovePayment(Payment{InvoiceID: "inv", Amount: 4001}, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("remove more than paid: %v", err)
	}
	if err := inv.RemovePayment(Payment{InvoiceID: "other", Amount: 1}, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("another invoice's payment: %v", err)
	}
}

func TestPaymentsReachPaidAndReopenAsIssued(t *testing.T) {
	now := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	inv := &Invoice{ID: "inv", Status: InvoiceOverdue, Totals: Totals{Total: 500}}
	p := Payment{Amount: 500, ReceivedOn: NewDate(2026, 11, 24)}
	if err := inv.RecordPayment(&p, now); err != nil || inv.Status != InvoicePaid {
		t.Fatalf("full payment: %v %v", err, inv.Status)
	}
	if err := inv.RemovePayment(p, now); err != nil || inv.Status != InvoiceIssued {
		t.Fatalf("never e-mailed: back to issued, got %v %v", err, inv.Status)
	}

	zero := &Invoice{ID: "z", Status: InvoiceIssued}
	if p, err := zero.MarkPaid(NewDate(2026, 11, 24), now); err != nil || p.Amount != 0 || zero.Status != InvoicePaid {
		t.Fatalf("zero total: %v %+v %v", err, p, zero.Status)
	}
	overdue := &Invoice{ID: "o", Status: InvoiceOverdue, OverdueAt: now, SentAt: now, Totals: Totals{Total: 5}}
	full := Payment{Amount: 5, ReceivedOn: NewDate(2026, 11, 24)}
	if err := overdue.RecordPayment(&full, now); err != nil || overdue.RemovePayment(full, now) != nil || overdue.Status != InvoiceOverdue {
		t.Fatalf("was overdue: back to overdue, got %v %v", err, overdue.Status)
	}
	void := &Invoice{ID: "v", Status: InvoiceVoid}
	if err := void.RemovePayment(Payment{InvoiceID: "v", Amount: 1}, now); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("void: %v", err)
	}
}
