package domain

import (
	"strings"
	"testing"
)

const testToken = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO_-" // 43 characters

func TestQuoteResponseLinksAndHTML(t *testing.T) {
	l := NewQuoteResponseLinks("https://inv.example.com/", testToken)
	if l.Accept != "https://inv.example.com/q/"+testToken+"?answer=accept" || l.Decline != "https://inv.example.com/q/"+testToken+"?answer=decline" {
		t.Fatalf("links = %+v", l)
	}
	body := l.AppendTo("Dear <Acme> & co,\n\nPlease find our quote.\n\nRegards\n")
	if !strings.Contains(body, "Regards\n\nTo accept this quote, open: "+l.Accept+"\nTo decline it, open: "+l.Decline+"\n") {
		t.Fatalf("text = %q", body)
	}
	h := QuoteEmailHTML(body, "Example & Sons")
	for _, want := range []string{
		`href="` + l.Accept + `"`, `href="` + l.Decline + `"`, ">Accept quote<", ">Decline quote<",
		"Dear &lt;Acme&gt; &amp; co,", "Example &amp; Sons",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if strings.Contains(h, "To accept this quote") {
		t.Error("the link lines stay in the HTML")
	}
	// No links, or links that are not this application's: plain text only.
	for _, b := range []string{
		"Hello",
		"To accept this quote, open: https://evil.example/x\nTo decline it, open: https://evil.example/y",
		"To accept this quote, open: javascript:alert(1)/q/" + testToken + "?answer=accept\nTo decline it, open: " + l.Decline,
		"To accept this quote, open: " + l.Accept + "\" onclick=\"x\nTo decline it, open: " + l.Decline,
	} {
		if QuoteEmailHTML(b, "B") != "" {
			t.Errorf("HTML made from %q", b)
		}
	}
}

func TestParsePublicURLAndAnswer(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {"\thttps://x.example/\n", "https://x.example"}, {"http://10.0.0.5:8080", "http://10.0.0.5:8080"}, {"https://h/app/", "https://h/app"},
	} {
		if got, err := ParsePublicURL(tc.in); err != nil || got != tc.want {
			t.Errorf("%q → %q %v", tc.in, got, err)
		}
	}
	for _, bad := range []string{"ftp://x", "x.example", "https://", "https://u:p@x", "https://x/?a=1", "https://x/#f", "javascript:alert(1)"} {
		if _, err := ParsePublicURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if a, err := ParseQuoteAnswer(" decline "); err != nil || a != QuoteAnswerDecline {
		t.Fatal(a, err)
	}
	if _, err := ParseQuoteAnswer("maybe"); err == nil {
		t.Fatal("maybe accepted")
	}
	s := DefaultQuoteSettings()
	s.PublicURL = "https://x.example/"
	if err := s.Validate(); err != nil || s.PublicURL != "https://x.example" {
		t.Fatalf("settings: %v %q", err, s.PublicURL)
	}
}
