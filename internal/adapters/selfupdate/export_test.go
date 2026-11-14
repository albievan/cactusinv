package selfupdate

import "crypto/ed25519"

// NewFor is New for a given binary instead of the running one.
func NewFor(dist, exe string) *Installer {
	i := New(dist, false)
	i.exe, i.exeErr = exe, nil
	return i
}

// NewForKey is NewFor checking signatures with key instead of the release key.
func NewForKey(dist, exe string, key ed25519.PublicKey) *Installer {
	i := NewFor(dist, exe)
	i.key = key
	return i
}

// ReleaseKeyForTest is the key a real installer checks signatures with.
func ReleaseKeyForTest() ed25519.PublicKey { return New("", false).key }
