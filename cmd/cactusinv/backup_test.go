package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/agecrypt"
)

func TestBackupKeygen(t *testing.T) {
	var out bytes.Buffer
	if err := backupCommand(context.Background(), config{}, []string{"keygen"}, nil, &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "# cactusInv backup_key, public key age1") || !strings.Contains(out.String(), "AGE-SECRET-KEY-1") {
		t.Fatalf("keygen = %q", out.String())
	}
	if s, err := agecrypt.New(out.String()); err != nil || !s.KeyConfigured() {
		t.Fatalf("the printed key file loads: %v", err)
	}
	if err := backupCommand(context.Background(), config{}, nil, nil, &out, false); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no subcommand: %v", err)
	}
}

func TestParseInterleaved(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	pf := fs.String("passphrase-file", "", "")
	pos, err := parseInterleaved(fs, []string{"backup.age", "--yes", "--passphrase-file", "p.txt"})
	if err != nil || !slices.Equal(pos, []string{"backup.age"}) || !*yes || *pf != "p.txt" {
		t.Fatalf("%v %v %v %q", pos, err, *yes, *pf)
	}
	if _, err := parseInterleaved(flag.NewFlagSet("y", flag.ContinueOnError), []string{"--nope"}); err == nil {
		t.Fatal("unknown flag")
	}
}

func TestReadPassphrase(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(p, []byte("a long enough passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readPassphrase(p); err != nil || got != "a long enough passphrase" {
		t.Fatalf("file: %q %v", got, err)
	}
	t.Setenv(passphraseEnv, "from the environment!")
	if got, _ := readPassphrase(""); got != "from the environment!" {
		t.Fatalf("env: %q", got)
	}
	if _, err := readPassphrase(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file")
	}
	f, err := openPath(filepath.Join(dir, "new.age"), true)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if _, err := openPath(filepath.Join(dir, "new.age"), true); err == nil {
		t.Fatal("never overwrites")
	}
}

func TestCatLegacy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cactusinv-20261001-020000.sql.gz"), []byte("dump"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := backupCommand(context.Background(), config{BackupDir: dir}, []string{"cat-legacy", "cactusinv-20261001-020000.sql.gz"}, nil, &out, false); err != nil || out.String() != "dump" {
		t.Fatalf("%q %v", out.String(), err)
	}
	for _, bad := range []string{"../etc/passwd", "cactusinv-20261015T000000Z-s14-manual.age", "x.sql.gz"} {
		if err := backupCommand(context.Background(), config{BackupDir: dir}, []string{"cat-legacy", bad}, nil, &out, false); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestConfirmRestore(t *testing.T) {
	var out bytes.Buffer
	if !confirmRestore(strings.NewReader("RESTORE\n"), &out) || confirmRestore(strings.NewReader("yes\n"), &out) {
		t.Fatal("only RESTORE confirms")
	}
}
