// Package smtp implements ports.Mailer over SMTP with STARTTLS, implicit TLS, or (for a
// local Mailpit only) plain connections.
package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/albievan/cactusinve/internal/ports"
)

// TLS modes.
const (
	TLSStartTLS = "starttls"
	TLSImplicit = "tls"
	TLSNone     = "none"
)

// Config configures the mailer.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Example Software Consulting <billing@example.com>"
	TLS      string // starttls (default) | tls | none
	Timeout  time.Duration
	// TLSConfig overrides the TLS client configuration (tests).
	TLSConfig *tls.Config
}

// Mailer sends e-mail over SMTP.
type Mailer struct {
	cfg  Config
	from *mail.Address
}

var _ ports.Mailer = (*Mailer)(nil)

// New validates the configuration.
func New(cfg Config) (*Mailer, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("smtp.New: from address: %w", err)
	}
	if cfg.Host == "" {
		return nil, errors.New("smtp.New: host is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.TLS == "" {
		cfg.TLS = TLSStartTLS
	}
	if cfg.TLS != TLSStartTLS && cfg.TLS != TLSImplicit && cfg.TLS != TLSNone {
		return nil, fmt.Errorf("smtp.New: unknown TLS mode %q", cfg.TLS)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Mailer{cfg: cfg, from: from}, nil
}

// Send implements ports.Mailer.
func (m *Mailer) Send(ctx context.Context, msg ports.Mail) error {
	raw, err := Build(m.from, msg, time.Now())
	if err != nil {
		return fmt.Errorf("smtp.Send: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
	defer cancel()
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	tlsCfg := m.cfg.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	}

	var conn net.Conn
	if m.cfg.TLS == TLSImplicit {
		conn, err = (&tls.Dialer{Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp.Send: dial: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	c, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp.Send: greeting: %w", err)
	}
	defer c.Close()
	if err := c.Hello(helloName(m.from.Address)); err != nil {
		return fmt.Errorf("smtp.Send: hello: %w", err)
	}
	if m.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp.Send: server does not offer STARTTLS")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("smtp.Send: starttls: %w", err)
		}
	}
	if m.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)); err != nil {
			return fmt.Errorf("smtp.Send: auth: %w", err)
		}
	}
	if err := c.Mail(m.from.Address); err != nil {
		return fmt.Errorf("smtp.Send: mail from: %w", err)
	}
	for _, rcpt := range append(append([]string{}, msg.To...), msg.CC...) {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp.Send: rcpt: %w", err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp.Send: data: %w", err)
	}
	if _, err := w.Write(raw); err != nil {
		return fmt.Errorf("smtp.Send: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp.Send: end data: %w", err)
	}
	return c.Quit()
}

func helloName(from string) string {
	if _, domain, ok := strings.Cut(from, "@"); ok && domain != "" {
		return domain
	}
	return "localhost"
}

// Build renders a MIME message: a quoted-printable text body plus base64 attachments.
// The Message-ID is derived from msg.MessageID (the outbox row) so a duplicate delivery
// after a crash is recognisable (ADR-004).
func Build(from *mail.Address, msg ports.Mail, now time.Time) ([]byte, error) {
	for _, a := range append(append([]string{}, msg.To...), msg.CC...) {
		if strings.ContainsAny(a, "\r\n") {
			return nil, errors.New("invalid recipient")
		}
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	h("From", from.String())
	h("To", strings.Join(msg.To, ", "))
	if len(msg.CC) > 0 {
		h("Cc", strings.Join(msg.CC, ", "))
	}
	if msg.ReplyTo != "" && !strings.ContainsAny(msg.ReplyTo, "\r\n") {
		h("Reply-To", msg.ReplyTo)
	}
	h("Subject", mime.QEncoding.Encode("utf-8", strings.NewReplacer("\r", " ", "\n", " ").Replace(msg.Subject)))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", messageID(msg.MessageID, from.Address))
	h("MIME-Version", "1.0")
	h("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	buf.WriteString("\r\n")

	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {"text/plain; charset=utf-8"},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return nil, err
	}
	qp := quotedprintable.NewWriter(part)
	if _, err := qp.Write([]byte(strings.ReplaceAll(msg.Body, "\n", "\r\n"))); err != nil {
		return nil, err
	}
	if err := qp.Close(); err != nil {
		return nil, err
	}
	for _, a := range msg.Attachments {
		name := mime.QEncoding.Encode("utf-8", a.Filename)
		part, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {a.ContentType + "; name=\"" + name + "\""},
			"Content-Disposition":       {"attachment; filename=\"" + name + "\""},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			if _, err := part.Write([]byte(enc[:76] + "\r\n")); err != nil {
				return nil, err
			}
			enc = enc[76:]
		}
		if _, err := part.Write([]byte(enc + "\r\n")); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func messageID(id, from string) string {
	_, domain, _ := strings.Cut(from, "@")
	if domain == "" {
		domain = "cactusinv.local"
	}
	if id == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		id = base64.RawURLEncoding.EncodeToString(b)
	}
	return "<" + id + "@" + domain + ">"
}
