package domain

import (
	"errors"
	"testing"
)

func TestSMTPSettings(t *testing.T) {
	g := GmailSMTP()
	if g.Host != "smtp.gmail.com" || g.Port != 587 || g.TLS != "starttls" {
		t.Fatalf("gmail preset = %+v", g)
	}
	if err := g.Validate(); err != nil {
		t.Fatal("disabled config may be incomplete")
	}
	g.Enabled, g.Username = true, "me@gmail.com"
	if err := g.Validate(); err != nil || g.From != "me@gmail.com" {
		t.Fatalf("from defaults to username: %v %q", err, g.From)
	}
	for _, bad := range []SMTPSettings{
		{Enabled: true, Host: "", Port: 587, From: "a@b.example"},
		{Enabled: true, Host: "smtp.x", Port: 0, From: "a@b.example"},
		{Enabled: true, Host: "smtp.x", Port: 587, From: "nope"},
		{Enabled: true, Host: "smtp.x", Port: 587, From: "a@b.example", TLS: "ssl"},
		{Port: 70000},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrValidation) {
			t.Errorf("Validate(%+v) = %v", bad, err)
		}
	}
}

func TestEmailSenderWarnings(t *testing.T) {
	const (
		gmail = "Gmail sends as albievan@gmail.com unless accounts@primesoft.co.za is a verified \"Send mail as\" address of that account (Gmail → Settings → Accounts → Send mail as)."
		ms365 = "Microsoft 365 may refuse to send as accounts@primesoft.co.za unless it is the mailbox's own address or me@primesoft.co.za has Send As permission for it."
	)
	tests := []struct {
		name string
		s    SMTPSettings
		want string // "" = no warning
	}{
		{"gmail, other address", SMTPSettings{Host: "smtp.gmail.com", Username: "albievan@gmail.com", From: "accounts@primesoft.co.za"}, gmail},
		{"gmail, name and other address", SMTPSettings{Host: "smtp.gmail.com", Username: "albievan@gmail.com", From: "Primesoft Accounts <accounts@primesoft.co.za>"}, gmail},
		{"gmail host in capitals with a trailing dot", SMTPSettings{Host: " SMTP.Gmail.com. ", Username: "albievan@gmail.com", From: "accounts@primesoft.co.za"}, gmail},
		{"googlemail host", SMTPSettings{Host: "smtp.googlemail.com", Username: "albievan@gmail.com", From: "accounts@primesoft.co.za"}, gmail},
		{"gmail, same address in other case", SMTPSettings{Host: "smtp.gmail.com", Username: "AlbieVan@gmail.com", From: "Albie <albievan@GMAIL.com>"}, ""},
		{"gmail, Send as empty defaults to user name", SMTPSettings{Host: "smtp.gmail.com", Username: "albievan@gmail.com"}, ""},
		{"gmail, no user name", SMTPSettings{Host: "smtp.gmail.com", From: "accounts@primesoft.co.za"}, ""},
		{"gmail, unparsable Send as", SMTPSettings{Host: "smtp.gmail.com", Username: "albievan@gmail.com", From: "not an address"}, ""},
		{"workspace relay may send for the domain", SMTPSettings{Host: "smtp-relay.gmail.com", Username: "albievan@gmail.com", From: "accounts@primesoft.co.za"}, ""},
		{"office365, other address", SMTPSettings{Host: "smtp.office365.com", Username: "me@primesoft.co.za", From: "accounts@primesoft.co.za"}, ms365},
		{"outlook.com host", SMTPSettings{Host: "smtp-mail.outlook.com", Username: "me@primesoft.co.za", From: "Accounts <accounts@primesoft.co.za>"}, ms365},
		{"office365, same address", SMTPSettings{Host: "smtp.office365.com", Username: "accounts@primesoft.co.za", From: "accounts@primesoft.co.za"}, ""},
		{"other provider", SMTPSettings{Host: "smtp.example.com", Username: "me@example.com", From: "billing@example.com"}, ""},
		{"empty", SMTPSettings{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EmailSenderWarnings(tt.s)
			if got == nil {
				t.Fatal("never nil")
			}
			if tt.want == "" && len(got) != 0 || tt.want != "" && (len(got) != 1 || got[0] != tt.want) {
				t.Fatalf("EmailSenderWarnings(%+v) = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}
