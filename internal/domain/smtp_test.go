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
