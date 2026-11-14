// Command releasesign signs a release's SHA256SUMS with the release key (M45), so an
// installation updating itself (System Settings → Install and restart) can check the
// checksums came from the owner of that key, not merely from the download site.
//
//	go run ./tools/releasesign keygen KEYFILE   # once: writes the private key, prints the public one
//	go run ./tools/releasesign sign KEYFILE FILE # writes FILE.sig (base64 Ed25519 signature)
//
// The private key never goes in the repository; keep a copy somewhere safe: without it no
// further release can be installed by an installation's own update button.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releasesign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	switch {
	case len(args) == 2 && args[0] == "keygen":
		return keygen(args[1])
	case len(args) == 3 && args[0] == "sign":
		return sign(args[1], args[2])
	}
	return errors.New("usage: releasesign keygen KEYFILE | sign KEYFILE FILE")
}

func keygen(file string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil { //nolint:gosec // the key file named on the command line
		return err
	}
	// O_EXCL: an existing key is never overwritten (it would orphan every installation).
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // the key file named on the command line
	if err != nil {
		return err
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func sign(keyFile, file string) error {
	raw, err := os.ReadFile(keyFile) //nolint:gosec // the release key named on the command line
	if err != nil {
		return fmt.Errorf("%w (create one with: go run ./tools/releasesign keygen %s)", err, keyFile)
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return errors.New("the key file does not hold an Ed25519 key")
	}
	data, err := os.ReadFile(file) //nolint:gosec // the file to sign, named on the command line
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
	return os.WriteFile(file+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644) //nolint:gosec // a public signature
}
