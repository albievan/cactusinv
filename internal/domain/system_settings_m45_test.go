package domain

import "testing"

func TestExternalAddressAndHSTS(t *testing.T) {
	s := DefaultSystemSettings(0)
	s.ExternalURL = " https://invoice.primesoft.co.za/ "
	s.HSTS = true
	if err := s.Validate(); err != nil || s.ExternalURL != "https://invoice.primesoft.co.za" {
		t.Fatalf("%q %v", s.ExternalURL, err)
	}
	for name, bad := range map[string]SystemSettings{
		"not a URL":            {ExternalURL: "invoice.primesoft.co.za"},
		"query":                {ExternalURL: "https://x.example/?a=1"},
		"HSTS over plain http": {ExternalURL: "http://x.example", HSTS: true},
	} {
		bad.SessionMax = DefaultSessionMax
		if err := bad.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	ok := SystemSettings{SessionMax: DefaultSessionMax, HSTS: true} // no address: the switch alone
	if err := ok.Validate(); err != nil {
		t.Fatalf("HSTS without an address: %v", err)
	}
}
