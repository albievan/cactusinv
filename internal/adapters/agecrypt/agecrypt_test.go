package agecrypt

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/albievan/cactusinv/internal/domain"
)

const pass = "a long enough passphrase"

func newKeyed(t testing.TB) *Sealer {
	t.Helper()
	key, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(key, WithWorkFactor(10))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func seal(t testing.TB, open func(io.Writer) (io.WriteCloser, error), plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := open(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readAll(s *Sealer, data []byte, passphrase string) ([]byte, error) {
	r, err := s.Open(bytes.NewReader(data), passphrase)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

func TestRoundTrips(t *testing.T) {
	s := newKeyed(t)
	plain := bytes.Repeat([]byte("invoice rows "), 10000) // several 64 KiB chunks
	withKey := seal(t, s.SealWithKey, plain)
	if got, err := readAll(s, withKey, ""); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("key round trip: %v", err)
	}
	withPass := seal(t, func(w io.Writer) (io.WriteCloser, error) { return s.SealWithPassphrase(w, pass) }, plain)
	if got, err := readAll(s, withPass, pass); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("passphrase round trip: %v", err)
	}
	if got, err := readAll(s, withKey, pass); err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("a passphrase does not stop the server key from opening its own backups: %v", err)
	}
	if bytes.Contains(withKey, []byte("invoice rows")) || bytes.Contains(withPass, []byte("invoice rows")) {
		t.Fatal("ciphertext contains plaintext")
	}
	if !strings.HasPrefix(s.Recipient(), "age1") || !s.KeyConfigured() {
		t.Fatalf("recipient %q", s.Recipient())
	}
	// The age CLI's own library opens a stored backup with the key file (age -d -i).
	key, _ := GenerateKey()
	k, _ := New(key)
	ids, _ := age.ParseIdentities(strings.NewReader(key))
	r, err := age.Decrypt(bytes.NewReader(seal(t, k.SealWithKey, []byte("x"))), ids...)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "x" {
		t.Fatal("age -d")
	}
}

func TestOpenFailuresAreOneError(t *testing.T) {
	s := newKeyed(t)
	plain := bytes.Repeat([]byte("x"), 200000)
	withKey := seal(t, s.SealWithKey, plain)
	withPass := seal(t, func(w io.Writer) (io.WriteCloser, error) { return s.SealWithPassphrase(w, pass) }, plain)
	flip := func(b []byte, i int) []byte {
		c := bytes.Clone(b)
		c[i] ^= 0x01
		return c
	}
	other := newKeyed(t)
	cases := []struct {
		name       string
		sealer     *Sealer
		data       []byte
		passphrase string
	}{
		{"wrong passphrase", s, withPass, "another long passphrase"},
		{"other server key", other, withKey, ""},
		{"passphrase file without passphrase", s, withPass, ""},
		{"flipped payload byte", s, flip(withKey, len(withKey)-5000), ""},
		{"flipped header byte", s, flip(withKey, 40), ""},
		{"truncated", s, withKey[:len(withKey)-100], ""},
		{"truncated to a chunk boundary", s, withKey[:len(withKey)/2], ""},
		{"empty", s, nil, ""},
		{"not age", s, []byte("-- MariaDB dump\nCREATE TABLE x;"), ""},
		{"appended data", s, append(bytes.Clone(withKey), 0), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := readAll(c.sealer, c.data, c.passphrase); !errors.Is(err, domain.ErrBackupInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// TestWorkFactorIsCapped: a file demanding more scrypt work than DefaultWorkFactor is
// refused before any work is done (an upload cannot exhaust the server's memory).
func TestWorkFactorIsCapped(t *testing.T) {
	s := newKeyed(t)
	data := seal(t, func(w io.Writer) (io.WriteCloser, error) { return s.SealWithPassphrase(w, pass) }, []byte("x"))
	edited := bytes.Replace(data, []byte(" 10\n"), []byte(" 30\n"), 1)
	if bytes.Equal(edited, data) {
		t.Fatal("work factor not found in header")
	}
	if _, err := readAll(s, edited, pass); !errors.Is(err, domain.ErrBackupInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfiguration(t *testing.T) {
	none, err := New("")
	if err != nil || none.KeyConfigured() || none.Recipient() != "" {
		t.Fatal("no key")
	}
	if _, err := none.SealWithKey(io.Discard); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatalf("seal without key: %v", err)
	}
	if _, err := none.Open(strings.NewReader("x"), ""); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatalf("open without key or passphrase: %v", err)
	}
	if _, err := none.SealWithPassphrase(io.Discard, "short"); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("short passphrase: %v", err)
	}
	for _, bad := range []string{"not a key", "AGE-SECRET-KEY-1XYZ"} {
		if _, err := New(bad); err == nil || strings.Contains(err.Error(), bad) {
			t.Errorf("%q: %v (and the error must not echo the key)", bad, err)
		}
	}
	k1, _ := GenerateKey()
	k2, _ := GenerateKey()
	if _, err := New(k1 + k2); err == nil {
		t.Fatal("two identities")
	}
	if _, err := New(k1, WithWorkFactor(19)); err == nil {
		t.Fatal("work factor above the cap")
	}
	if _, err := New(k1, WithWorkFactor(0)); err == nil {
		t.Fatal("work factor 0")
	}
	if s, err := New("# backup key\n" + k1); err != nil || !s.KeyConfigured() {
		t.Fatalf("comment lines: %v", err)
	}
	pq, err := age.GenerateHybridIdentity()
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(pq.String())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := readAll(h, seal(t, h.SealWithKey, []byte("pq")), ""); err != nil || string(got) != "pq" {
		t.Fatalf("hybrid key: %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	key, _ := GenerateKey()
	s, _ := New(key, WithWorkFactor(1))
	var buf bytes.Buffer
	w, _ := s.SealWithKey(&buf)
	_, _ = w.Write([]byte("hello backup"))
	_ = w.Close()
	f.Add(buf.Bytes(), false)
	buf.Reset()
	w, _ = s.SealWithPassphrase(&buf, pass)
	_, _ = w.Write([]byte("hello backup"))
	_ = w.Close()
	f.Add(buf.Bytes(), true)
	f.Add([]byte("age-encryption.org/v1\n-> X25519 abc\n--- xyz\n"), false)
	f.Fuzz(func(t *testing.T, data []byte, withPass bool) {
		p := ""
		if withPass {
			p = pass
		}
		r, err := s.Open(bytes.NewReader(data), p)
		if err != nil {
			if !errors.Is(err, domain.ErrBackupInvalid) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		if _, err := io.Copy(io.Discard, io.LimitReader(r, 1<<20)); err != nil && !errors.Is(err, domain.ErrBackupInvalid) {
			t.Fatalf("unexpected read error kind: %v", err)
		}
	})
}
