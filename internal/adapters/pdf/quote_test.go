package pdf

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

func quoteDoc(t *testing.T, lines int) ports.QuoteDocument {
	t.Helper()
	issue := domain.NewDate(2026, 11, 12)
	content := domain.QuoteContent{
		IssueDate: issue, ValidUntil: issue.AddDays(30), SalesPerson: "Albie", Reference: "Website rebuild",
		BillTo: domain.Party{Name: "Prospect Holdings (Pty) Ltd", Address: "12 Long Street\nCape Town\n8001", Attention: "Thandi"},
		Notes:  "Hosting is quoted for twelve months.", Terms: "50% deposit on acceptance; balance on delivery.", TaxRate: 1500,
	}
	for i := range lines {
		content.Lines = append(content.Lines, domain.Line{
			Description: "Design and build, phase " + strconv.Itoa(i+1), Quantity: 2000, UnitPrice: 450000, Discount: 1000,
		})
	}
	q, err := domain.NewQuote("q", "Q-17/26", domain.QuoteFor{AdhocCustomerID: "a", Currency: "ZAR"}, content, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return ports.QuoteDocument{
		Quote: q, Business: domain.BusinessProfile{Name: "Example Software Consulting", AddressLines: "P. O. Box 1000\nNorthgate"},
		Currency: domain.Currency{Code: "ZAR", Symbol: "R", MinorUnits: 2},
	}
}

func renderQuote(t *testing.T, r Renderer, doc ports.QuoteDocument) []byte {
	t.Helper()
	out, err := r.RenderQuote(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Fatal("not a PDF")
	}
	return out
}

func TestRenderQuote(t *testing.T) {
	r, runs := tracer()
	doc := quoteDoc(t, 2)
	out := renderQuote(t, r, doc)
	save(t, "quote.pdf", out)
	txt := textOf(*runs)
	for _, want := range []string{
		"Example Software Consulting", "Q U O T A T I O N", "Q-17/26", "12 November 2026", "Valid until", "12 December 2026",
		"QUOTATION FOR", "Prospect Holdings (Pty) Ltd", "Attn: Thandi", "Website rebuild", "Verification code",
		doc.Quote.Fingerprint(), "Design and build, phase 2", "4,500.00", "8,100.00", "Discount", "VAT 15%", "Total ZAR",
		"18,630.00", "VALIDITY", "This quotation is valid until 12 December 2026.", "Hosting is quoted for twelve months.",
		"Terms: 50% deposit on acceptance; balance on delivery.",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("quote PDF is missing %q", want)
		}
	}
	// A quote is not a demand for payment.
	for _, absent := range []string{"PAYMENT DETAILS", "Amount due", "Due", "EXPIRED", "DRAFT"} {
		if strings.Contains(txt, absent) {
			t.Errorf("quote PDF must not contain %q", absent)
		}
	}
}

// pValue is the permission flags of a PDF's encryption dictionary.
var pValue = regexp.MustCompile(`/P (-?\d+)`)

func TestQuoteIsPrintOnly(t *testing.T) {
	out := renderQuote(t, Renderer{}, quoteDoc(t, 1))
	if !bytes.Contains(out, []byte("/Encrypt")) || !bytes.Contains(out, []byte("/Filter /Standard")) || !bytes.Contains(out, []byte("/CFM /AESV2")) {
		t.Fatal("the quote PDF is not encrypted with AES")
	}
	m := pValue.FindSubmatch(out)
	if m == nil {
		t.Fatal("no permissions in the encryption dictionary")
	}
	p, _ := strconv.Atoi(string(m[1]))
	// PDF permission bits (ISO 32000-1, table 22): 3 print, 4 modify, 5 copy, 6 annotate.
	bit := func(n uint) bool { return uint32(int32(p))&(1<<(n-1)) != 0 }
	if !bit(3) || !bit(12) || bit(4) || bit(5) || bit(6) || bit(9) || bit(10) || bit(11) {
		t.Fatalf("permissions %d: want print only", p)
	}
	// The owner password is random: two renders of one quote are encrypted differently, so
	// the password of one cannot be learnt to unlock another.
	again := renderQuote(t, Renderer{}, quoteDoc(t, 1))
	o := regexp.MustCompile(`/O <([0-9a-f]{64})>`)
	if m1, m2 := o.FindSubmatch(out), o.FindSubmatch(again); m1 == nil || m2 == nil || bytes.Equal(m1[1], m2[1]) {
		t.Fatal("the owner password is not random")
	}
	// Nothing printed is readable without decrypting.
	if bytes.Contains(out, []byte("Prospect Holdings")) {
		t.Fatal("text is stored in the clear")
	}
	// Invoices are not encrypted (only quotes were asked for).
	inv, _ := render(t, reference(t))
	if bytes.Contains(inv, []byte("/Encrypt")) {
		t.Fatal("an invoice PDF is encrypted")
	}
}

func TestQuoteStatusAndPages(t *testing.T) {
	doc := quoteDoc(t, 60) // runs on to further pages
	doc.Expired = true
	r, runs := tracer()
	out := renderQuote(t, r, doc)
	save(t, "quote-long.pdf", out)
	txt := textOf(*runs)
	if !strings.Contains(txt, "Quotation Q-17/26 (continued)") || !strings.Contains(txt, "EXPIRED") || !strings.Contains(txt, "Page 2 of") {
		t.Fatal("a long expired quote is not continued and marked")
	}
	last := 0
	for _, run := range *runs {
		last = max(last, run.page)
		if run.y > pageH-10 {
			t.Fatalf("text below the page: %+v", run)
		}
	}
	if last < 2 {
		t.Fatalf("pages = %d", last)
	}
	for _, tc := range []struct {
		apply func(*domain.Quote)
		want  string
	}{
		{func(q *domain.Quote) { _ = q.Accept(time.Now()) }, "ACCEPTED"},
		{func(q *domain.Quote) { _ = q.Decline("", time.Now()) }, "DECLINED"},
		{func(q *domain.Quote) { q.CustomerID = "c"; _ = q.Convert("i", time.Now()) }, "INVOICED"},
	} {
		d := quoteDoc(t, 1)
		tc.apply(d.Quote)
		r, runs := tracer()
		renderQuote(t, r, d)
		if txt := textOf(*runs); !strings.Contains(txt, tc.want) {
			t.Errorf("status %s is not shown", tc.want)
		}
	}
}

// A quote with several links in its notes and terms, compressed as served; saved for a
// look with a PDF reader (CACTUSINV_PDF_OUT).
func TestQuoteWithLinks(t *testing.T) {
	doc := quoteDoc(t, 3)
	notes, err := domain.CleanNotes("notes", `<p>See <a href="https://example.com/portfolio">our portfolio</a>, <a href="mailto:sales@example.com">write to sales</a> or <a href="https://example.com/faq">the FAQ</a>.</p>`)
	if err != nil {
		t.Fatal(err)
	}
	doc.Quote.Notes = notes
	doc.Quote.Terms = "Full terms at www.example.com/terms."
	out := renderQuote(t, Renderer{}, doc)
	save(t, "quote-links.pdf", out)
	if bytes.Contains(out, []byte("example.com")) {
		t.Fatal("a link is stored in the clear")
	}
}

// Every string of an encrypted quote decrypts on its own, as a reader decrypts it: the key
// is worked out from the document's /O, /P and /ID (there is no user password), each
// string with its object's key and IV. fpdf's own protection got every string after the
// first in an object wrong.
func TestQuoteStringsDecrypt(t *testing.T) {
	doc := quoteDoc(t, 1)
	notes, _ := domain.CleanNotes("notes", `<p><a href="https://a.example/1">one</a> <a href="https://b.example/2">two</a> <a href="mailto:c@example.com">three</a></p>`)
	doc.Quote.Notes = notes
	out := renderQuote(t, Renderer{}, doc)

	field := func(re string) []byte {
		m := regexp.MustCompile(re).FindSubmatch(out)
		if m == nil {
			t.Fatalf("no %s", re)
		}
		return m[1]
	}
	o, _ := hex.DecodeString(string(field(`/O <([0-9a-f]+)>`)))
	id, _ := hex.DecodeString(string(field(`/ID \[<([0-9a-f]+)>`)))
	p, _ := strconv.Atoi(string(field(`/P (-?\d+)`)))
	var pb [4]byte
	binary.LittleEndian.PutUint32(pb[:], uint32(int32(p))) //nolint:gosec // the permission flags are a 32-bit pattern
	if !bytes.Equal(pb[:], printOnlyBytes) {
		t.Fatalf("/P %d is not the bytes the key is made with", p)
	}
	h := md5.Sum(bytes.Join([][]byte{passwordPad, o, pb[:], id}, nil))
	for range 50 {
		h = md5.Sum(h[:16])
	}
	key := h[:16]

	decrypt := func(num int, data []byte) string {
		k := md5.Sum(append(append(append([]byte{}, key...), byte(num), byte(num>>8), byte(num>>16), 0, 0), "sAlT"...))
		block, _ := aes.NewCipher(k[:16])
		plain := make([]byte, len(data)-16)
		cipher.NewCBCDecrypter(block, data[:16]).CryptBlocks(plain, data[16:])
		return string(plain[:len(plain)-int(plain[len(plain)-1])])
	}
	objs := regexp.MustCompile(`(?s)(\d+) 0 obj\n(.*?)endobj`).FindAllSubmatch(out, -1)
	var uris []string
	var title string
	for _, ob := range objs {
		num, _ := strconv.Atoi(string(ob[1]))
		if bytes.Contains(ob[2], []byte("stream\n")) || bytes.Contains(ob[2], []byte("/Filter /Standard")) {
			continue
		}
		for _, m := range regexp.MustCompile(`/(URI|Title) <([0-9a-f]+)>`).FindAllSubmatch(ob[2], -1) {
			raw, _ := hex.DecodeString(string(m[2]))
			if string(m[1]) == "URI" {
				uris = append(uris, decrypt(num, raw))
			} else {
				title = decrypt(num, raw)
			}
		}
	}
	if want := []string{"https://a.example/1", "https://b.example/2", "mailto:c@example.com"}; !slices.Equal(uris, want) {
		t.Fatalf("links = %q, want %q", uris, want)
	}
	if !strings.Contains(title, "Q\x00u\x00o\x00t\x00a\x00t\x00i\x00o\x00n") { // UTF-16BE
		t.Fatalf("title = %q", title)
	}
}
