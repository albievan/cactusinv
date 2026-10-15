package domain

import "testing"

func TestStatement(t *testing.T) {
	c := cityDeep()
	mk := func(id, num string, status InvoiceStatus, due Date, total int64, cur CurrencyCode) Invoice {
		return Invoice{ID: id, Number: num, CustomerID: c.ID, Status: status, Currency: cur, IssueDate: due.AddDays(-30), DueDate: due, Totals: Totals{Total: total}}
	}
	asOf := NewDate(2026, 10, 15)
	invs := []Invoice{
		mk("a", "CD/3", InvoiceSent, NewDate(2026, 10, 20), 100, "ZAR"),   // current
		mk("b", "CD/2", InvoiceOverdue, NewDate(2026, 10, 1), 200, "ZAR"), // 14 days
		mk("c", "CD/1", InvoiceIssued, NewDate(2026, 6, 1), 400, "ZAR"),   // 136 days
		mk("d", "CD/0", InvoicePaid, NewDate(2026, 5, 1), 800, "ZAR"),     // paid: excluded
		mk("e", "CD/9", InvoiceSent, NewDate(2026, 9, 1), 1600, "USD"),    // other currency: excluded
		mk("f", "CD/8", InvoiceIssued, NewDate(2026, 8, 10), 3200, "ZAR"), // 66 days
		mk("g", "CD/7", InvoiceIssued, NewDate(2026, 9, 5), 6400, "ZAR"),  // 40 days
		{ID: "x", CustomerID: "other", Status: InvoiceSent, Currency: "ZAR", Totals: Totals{Total: 9}},
	}
	st := NewStatement(c, invs, asOf)
	if st.Total != 100+200+400+3200+6400 || len(st.Lines) != 5 {
		t.Fatalf("statement = %+v", st)
	}
	want := Aging{Current: 100, Days1to30: 200, Days31to60: 6400, Days61to90: 3200, Over90: 400}
	if st.Aging != want {
		t.Fatalf("aging = %+v, want %+v", st.Aging, want)
	}
	if st.Lines[0].Number != "CD/1" || st.Lines[0].DaysOverdue != 136 {
		t.Fatalf("oldest first: %+v", st.Lines[0])
	}
	if empty := NewStatement(c, nil, asOf); empty.Total != 0 || len(empty.Lines) != 0 {
		t.Fatal("empty statement")
	}
}
