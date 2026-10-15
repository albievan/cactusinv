package smtp

import (
	"bufio"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/albievan/cactusinve/internal/ports"
)

// fakeServer is a minimal SMTP server that records one transaction.
type fakeServer struct {
	ln       net.Listener
	mu       sync.Mutex
	rcpts    []string
	data     string
	features []string
	reject   string // command prefix to reject with 550
}

func startFake(t *testing.T, features ...string) *fakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeServer{ln: ln, features: features}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeServer) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeServer) handle(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(l string) { _, _ = io.WriteString(conn, l+"\r\n") }
	w("220 fake ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		if s.reject != "" && strings.HasPrefix(cmd, s.reject) {
			w("550 rejected")
			continue
		}
		switch {
		case strings.HasPrefix(cmd, "EHLO"):
			if len(s.features) == 0 {
				w("250 fake")
				continue
			}
			w("250-fake")
			for i, f := range s.features {
				sep := "-"
				if i == len(s.features)-1 {
					sep = " "
				}
				w("250" + sep + f)
			}
		case strings.HasPrefix(cmd, "MAIL"):
			w("250 ok")
		case strings.HasPrefix(cmd, "RCPT"):
			s.mu.Lock()
			s.rcpts = append(s.rcpts, strings.TrimSpace(line[len("RCPT TO:"):]))
			s.mu.Unlock()
			w("250 ok")
		case cmd == "DATA":
			w("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			w("250 queued")
		case cmd == "QUIT":
			w("221 bye")
			return
		default:
			w("250 ok")
		}
	}
}

func mailFixture() ports.Mail {
	return ports.Mail{
		MessageID: "0190-abc", To: []string{"juanita@acme.example"}, CC: []string{"accounts@acme.example"},
		ReplyTo: "billing@example.com", Subject: "Invoice CD/1195/26 – R 3 251,16",
		Body:        "Dear Acme,\nPlease find attached invoice CD/1195/26.\n",
		Attachments: []ports.Attachment{{Filename: "Inv_no_CD_1195_26.pdf", ContentType: "application/pdf", Data: []byte(strings.Repeat("%PDF-1.3 ", 40))}},
	}
}

func TestSendPlainAndParseMIME(t *testing.T) {
	s := startFake(t)
	m, err := New(Config{Host: "127.0.0.1", Port: s.port(), From: "Example Software Consulting <billing@example.com>", TLS: TLSNone})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Send(context.Background(), mailFixture()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rcpts) != 2 {
		t.Fatalf("rcpts = %v", s.rcpts)
	}
	msg, err := mail.ReadMessage(strings.NewReader(s.data))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	if subj != "Invoice CD/1195/26 – R 3 251,16" || msg.Header.Get("Message-ID") != "<0190-abc@example.com>" ||
		msg.Header.Get("Reply-To") != "billing@example.com" || msg.Header.Get("Cc") != "accounts@acme.example" {
		t.Fatalf("headers = %v / %q", msg.Header, subj)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	body, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(body)
	if !strings.Contains(string(text), "Please find attached invoice CD/1195/26.") {
		t.Fatalf("body = %q", text)
	}
	att, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if att.FileName() != "Inv_no_CD_1195_26.pdf" {
		t.Fatalf("filename = %q", att.FileName())
	}
}

func TestSendErrors(t *testing.T) {
	if _, err := New(Config{Host: "x", From: "not an address"}); err == nil {
		t.Fatal("bad from")
	}
	if _, err := New(Config{From: "a@b.example"}); err == nil {
		t.Fatal("no host")
	}
	if _, err := New(Config{Host: "x", From: "a@b.example", TLS: "ssl3"}); err == nil {
		t.Fatal("bad tls mode")
	}

	// STARTTLS required but not offered.
	s := startFake(t, "8BITMIME")
	m, _ := New(Config{Host: "127.0.0.1", Port: s.port(), From: "a@b.example"})
	if err := m.Send(context.Background(), mailFixture()); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("starttls: %v", err)
	}
	// Recipient rejected.
	s2 := startFake(t)
	s2.reject = "RCPT"
	m2, _ := New(Config{Host: "127.0.0.1", Port: s2.port(), From: "a@b.example", TLS: TLSNone})
	if err := m2.Send(context.Background(), mailFixture()); err == nil || !strings.Contains(err.Error(), "rcpt") {
		t.Fatalf("rcpt: %v", err)
	}
	// Nothing listening.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	m3, _ := New(Config{Host: "127.0.0.1", Port: port, From: "a@b.example", TLS: TLSNone, Timeout: time.Second})
	if err := m3.Send(context.Background(), mailFixture()); err == nil {
		t.Fatal("dial error expected")
	}
	// Header injection through recipients is refused.
	bad := mailFixture()
	bad.To = []string{"a@b.example\r\nBcc: evil@x.example"}
	if _, err := Build(&mail.Address{Address: "a@b.example"}, bad, time.Now()); err == nil {
		t.Fatal("injection")
	}
	if id := messageID("", "nodomain"); !strings.HasSuffix(id, "@cactusinv.local>") {
		t.Fatalf("message id = %s", id)
	}
	if helloName("x") != "localhost" {
		t.Fatal("hello fallback")
	}
}
