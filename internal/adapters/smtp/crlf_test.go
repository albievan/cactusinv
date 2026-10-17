package smtp_test

import (
	"io"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/smtp"
	"github.com/albievan/cactusinv/internal/ports"
)

// A body with CRLF line ends (from the API or a settings import) keeps one line break per
// line, not two.
func TestCRLFBodyIsNotDoubleSpaced(t *testing.T) {
	raw, err := smtp.Build(&mail.Address{Address: "a@example.com"}, ports.Mail{MessageID: "m1", To: []string{"b@example.com"}, Subject: "S", Body: "Dear A,\r\n\r\nThanks\r\n"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h := strings.Index(string(raw), "text/plain")
	i := strings.Index(string(raw)[max(h, 0):], "\r\n\r\n")
	if h < 0 || i < 0 {
		t.Fatalf("no text part: %s", raw)
	}
	part := string(raw[h+i+4:])
	end := strings.Index(part, "\r\n--")
	if end < 0 {
		t.Fatalf("no part boundary: %q", part)
	}
	part = part[:end]
	text, err := readQP(part)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Dear A,\r\n\r\nThanks\r\n" {
		t.Fatalf("body %q", text)
	}
}

func readQP(s string) (string, error) {
	b, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(s)))
	return string(b), err
}
