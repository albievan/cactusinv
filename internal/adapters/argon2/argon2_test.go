package argon2

import (
	"strings"
	"testing"
)

func TestHashVerify(t *testing.T) {
	h := Hasher{Memory: 8 * 1024, Time: 1, Threads: 1}
	hash, err := h.Hash("correct horse battery")
	if err != nil || !strings.HasPrefix(hash, "$argon2id$v=19$m=8192,t=1,p=1$") {
		t.Fatalf("hash = %q %v", hash, err)
	}
	if ok, err := h.Verify(hash, "correct horse battery"); !ok || err != nil {
		t.Fatal("verify")
	}
	if ok, _ := h.Verify(hash, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	for _, bad := range []string{"", "$bcrypt$x", "$argon2id$v=1$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=x$a$b", "$argon2id$v=19$m=8,t=1,p=1$!!$b", "$argon2id$v=19$m=8,t=1,p=1$YQ$!!"} {
		if _, err := h.Verify(bad, "x"); err == nil {
			t.Errorf("Verify(%q) expected error", bad)
		}
	}
	if m, tt, p := (Hasher{}).params(); m != 65536 || tt != 3 || p != 2 {
		t.Fatal("defaults")
	}
}
