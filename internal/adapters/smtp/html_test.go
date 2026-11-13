package smtp_test

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/smtp"
	"github.com/albievan/cactusinv/internal/ports"
)

// With an HTML body the message is mixed(alternative(text, html), attachments) (M41): mail
// clients show the buttons, plain-text readers the links, and the PDF stays attached.
func TestHTMLAlternative(t *testing.T) {
	raw, err := smtp.Build(&mail.Address{Address: "a@example.com"}, ports.Mail{
		MessageID: "m1", To: []string{"b@example.com"}, Subject: "Quote",
		Body: "Dear B,\n\nTo accept: https://x/q/t", HTMLBody: `<p>Dear B,</p><a href="https://x/q/t">Accept quote</a>`,
		Attachments: []ports.Attachment{{Filename: "Quote_1.pdf", ContentType: "application/pdf", Data: []byte("%PDF")}},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	_, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	outer := multipart.NewReader(msg.Body, params["boundary"])
	first, err := outer.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	typ, altParams, _ := mime.ParseMediaType(first.Header.Get("Content-Type"))
	if typ != "multipart/alternative" {
		t.Fatalf("first part = %s", typ)
	}
	alt := multipart.NewReader(first, altParams["boundary"])
	var types, bodies []string
	for {
		p, err := alt.NextPart() // decodes quoted-printable
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p)
		types, bodies = append(types, strings.Split(p.Header.Get("Content-Type"), ";")[0]), append(bodies, string(b))
	}
	if strings.Join(types, ",") != "text/plain,text/html" || !strings.Contains(bodies[0], "To accept: https://x/q/t") || !strings.Contains(bodies[1], ">Accept quote</a>") {
		t.Fatalf("alternatives = %v %q", types, bodies)
	}
	att, err := outer.NextPart()
	if err != nil || !strings.Contains(att.Header.Get("Content-Disposition"), "Quote_1.pdf") {
		t.Fatalf("attachment: %v %v", err, att)
	}

	// Without HTML: one plain-text part as before.
	raw, _ = smtp.Build(&mail.Address{Address: "a@example.com"}, ports.Mail{MessageID: "m2", To: []string{"b@example.com"}, Subject: "S", Body: "Hi"}, time.Now())
	if strings.Contains(string(raw), "multipart/alternative") || strings.Contains(string(raw), "text/html") {
		t.Fatal("an alternative without HTML")
	}
}
