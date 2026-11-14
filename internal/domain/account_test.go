package domain

import (
	"strings"
	"testing"
	"time"
)

func TestProfileValidate(t *testing.T) {
	p := Profile{FirstName: " Albie ", LastName: " van der Merwe ", EmailAddress: " albie@example.com "}
	if err := p.Validate(); err != nil || p.FirstName != "Albie" || p.LastName != "van der Merwe" || p.EmailAddress != "albie@example.com" {
		t.Fatalf("%+v %v", p, err)
	}
	if err := (&Profile{}).Validate(); err != nil {
		t.Fatalf("an empty profile: %v", err)
	}
	for name, bad := range map[string]Profile{
		"long first name": {FirstName: strings.Repeat("a", 101)},
		"two lines":       {LastName: "a\nb"},
		"bad address":     {EmailAddress: "not-an-address"},
		"two addresses":   {EmailAddress: "a@x.example, b@x.example"},
	} {
		if err := bad.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestResetAddress(t *testing.T) {
	for _, tc := range []struct {
		u    User
		want string
	}{
		{User{Email: "admin"}, ""},
		{User{Email: "admin", EmailAddress: "albie@example.com"}, "albie@example.com"},
		{User{Email: "clerk@example.com"}, "clerk@example.com"},
		{User{Email: "clerk@example.com", EmailAddress: "home@example.com"}, "home@example.com"},
	} {
		if got := tc.u.ResetAddress(); got != tc.want {
			t.Errorf("%+v → %q, want %q", tc.u, got, tc.want)
		}
	}
}

func TestPasswordResetMail(t *testing.T) {
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	r := PasswordReset{CreatedAt: now, ExpiresAt: now.Add(PasswordResetTTL)}
	if !r.Usable(now.Add(59*time.Minute)) || r.Usable(now.Add(time.Hour)) {
		t.Fatal("valid for an hour")
	}
	r.UsedAt = now
	if r.Usable(now) {
		t.Fatal("a used link works")
	}
	link := PasswordResetLink("https://inv.example.com/", "TOKEN")
	if link != "https://inv.example.com/reset-password/TOKEN" {
		t.Fatalf("link = %q", link)
	}
	body := PasswordResetBody(User{Email: "admin", FirstName: "Albie"}, "Example Ltd", link)
	for _, want := range []string{"Hello Albie,", "the user admin in cactusInv for Example Ltd", "within 60 minutes", link, "ignore this e-mail"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q: %q", want, body)
		}
	}
	if !strings.Contains(PasswordResetBody(User{Email: "admin"}, "", link), "Hello admin,") || PasswordResetSubject("") != "Reset your cactusInv password" ||
		PasswordResetSubject("Ex\nample") != "Reset your cactusInv password for Ex ample" {
		t.Fatal("defaults")
	}
}
