package domain

import (
	"net/mail"
	"strings"
)

// SMTPSettings is the outgoing mail server configured in the app. The password is kept
// separately, encrypted.
type SMTPSettings struct {
	Enabled  bool
	Host     string
	Port     int
	Username string
	From     string // "Example Software <billing@example.com>"
	TLS      string // starttls | tls | none
	Version  int
}

// GmailSMTP is the default preset: Gmail with STARTTLS. Gmail needs an app password
// (Google Account → Security → App passwords).
func GmailSMTP() SMTPSettings {
	return SMTPSettings{Host: "smtp.gmail.com", Port: 587, TLS: "starttls"}
}

// Validate checks the settings. A disabled configuration may be incomplete.
func (s *SMTPSettings) Validate() error {
	s.Host = strings.TrimSpace(s.Host)
	s.Username = strings.TrimSpace(s.Username)
	s.From = strings.TrimSpace(s.From)
	if s.TLS == "" {
		s.TLS = "starttls"
	}
	if s.TLS != "starttls" && s.TLS != "tls" && s.TLS != "none" {
		return invalid("tls", "must be starttls, tls or none")
	}
	if s.Port < 0 || s.Port > 65535 {
		return invalid("port", "must be a TCP port")
	}
	if len(s.Host) > 255 || len(s.Username) > 254 || len(s.From) > 320 {
		return invalid("smtp", "value too long")
	}
	if !s.Enabled {
		return nil
	}
	if s.Host == "" || strings.ContainsAny(s.Host, " /:") {
		return invalid("host", "is required (a host name, e.g. smtp.gmail.com)")
	}
	if s.Port == 0 {
		return invalid("port", "is required")
	}
	if s.From == "" && s.Username != "" {
		s.From = s.Username
	}
	if a, err := mail.ParseAddress(s.From); err != nil || a.Address == "" {
		return invalid("from", "must be an e-mail address, optionally with a name")
	}
	return nil
}
