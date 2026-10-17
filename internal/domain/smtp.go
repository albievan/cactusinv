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
	// Go's SMTP client sends a password only over TLS (or to localhost): with "none" every
	// e-mail would fail to sign in.
	if s.TLS == "none" && s.Username != "" && !isLocalhost(s.Host) {
		return invalid("tls", "a user name and password are only sent encrypted: choose STARTTLS or TLS, or leave the user name empty")
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

func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" // as net/smtp's PlainAuth
}

// EmailSenderWarnings says when the provider is known not to send as the Send-as address
// as configured: Gmail rewrites From to the signed-in account unless the address is a
// verified "Send mail as" alias, and Microsoft 365 may refuse unless it is the mailbox's own
// address or the mailbox has Send As permission. The alias list lives at the provider, so
// this warns and never refuses; it can warn needlessly (a Microsoft sign-in name that is not
// the primary address, a dotted or googlemail.com spelling of a Gmail address).
// smtp-relay.gmail.com is not covered: the Workspace relay may send for the domain. The
// result is never nil.
func EmailSenderWarnings(s SMTPSettings) []string {
	warnings := []string{}
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s.Host)), ".")
	user := strings.TrimSpace(s.Username)
	from := strings.TrimSpace(s.From)
	if user == "" || from == "" {
		return warnings // no account to compare with, or Send as defaults to the user name
	}
	a, err := mail.ParseAddress(from)
	if err != nil || strings.EqualFold(a.Address, user) {
		return warnings
	}
	switch host {
	case "smtp.gmail.com", "smtp.googlemail.com":
		warnings = append(warnings, "Gmail sends as "+user+" unless "+a.Address+
			" is a verified \"Send mail as\" address of that account (Gmail → Settings → Accounts → Send mail as).")
	case "smtp.office365.com", "smtp-mail.outlook.com":
		warnings = append(warnings, "Microsoft 365 may refuse to send as "+a.Address+
			" unless it is the mailbox's own address or "+user+" has Send As permission for it.")
	}
	return warnings
}
