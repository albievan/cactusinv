package smtp

import (
	"context"
	"mime"
	"net/mail"
	"strings"
	"testing"
	"time"
)

// A server that accepted the message but fails QUIT has still got it: no error, so the
// outbox does not send it again.
func TestQuitFailureAfterAcceptanceIsNotAnError(t *testing.T) {
	s := startFake(t)
	s.reject = "QUIT"
	m, _ := New(Config{Host: "127.0.0.1", Port: s.port(), From: "a@b.example", TLS: TLSNone})
	if err := m.Send(context.Background(), mailFixture()); err != nil {
		t.Fatalf("send: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == "" {
		t.Fatal("the message was delivered")
	}
}

func TestLongHeadersAreFolded(t *testing.T) {
	msg := mailFixture()
	msg.Subject = strings.Repeat("Счёт за услуги ", 25) // ~375 characters of Cyrillic
	for i := range 20 {
		msg.CC = append(msg.CC, strings.Repeat("x", 40)+string(rune('a'+i))+"@example.com")
	}
	raw, err := Build(&mail.Address{Address: "a@b.example"}, msg, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		if len(l) > 998 {
			t.Fatalf("header line of %d bytes: %.60q…", len(l), l)
		}
	}
	got, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(got.Header.Get("Subject"))
	if err != nil || subject != strings.TrimSpace(msg.Subject) && subject != msg.Subject {
		t.Fatalf("subject round trip: %q %v", subject, err)
	}
	if cc, err := got.Header.AddressList("Cc"); err != nil || len(cc) != len(msg.CC) {
		t.Fatalf("cc round trip: %d %v", len(cc), err)
	}
}
