package aesgcm

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	c, err := New(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := c.Encrypt([]byte("gmail app password"))
	if err != nil || strings.Contains(string(ct), "gmail") {
		t.Fatal("encrypt")
	}
	pt, err := c.Decrypt(ct)
	if err != nil || string(pt) != "gmail app password" {
		t.Fatalf("decrypt = %q %v", pt, err)
	}
	other, _ := New(strings.Repeat("cd", 32))
	if _, err := other.Decrypt(ct); err == nil {
		t.Fatal("wrong key must fail")
	}
	if _, err := c.Decrypt([]byte{1}); err == nil {
		t.Fatal("short")
	}
	for _, bad := range []string{"", "zz", strings.Repeat("ab", 16)} {
		if _, err := New(bad); err == nil {
			t.Errorf("New(%q) should fail", bad)
		}
	}
}
