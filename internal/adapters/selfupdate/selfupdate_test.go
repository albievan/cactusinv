//go:build !windows

package selfupdate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/selfupdate"
)

func script(version string) []byte {
	return []byte("#!/bin/sh\necho '" + version + " (build 40)'\n")
}

// publish serves dist/ with one binary and its SHA256SUMS line (sum may be forced).
func publish(t *testing.T, body []byte, sum string) string {
	t.Helper()
	if sum == "" {
		h := sha256.Sum256(body)
		sum = hex.EncodeToString(h[:])
	}
	asset := selfupdate.AssetName(runtime.GOOS, runtime.GOARCH)
	mux := http.NewServeMux()
	mux.HandleFunc("/dist/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sum + "  " + asset + "\n" + strings.Repeat("0", 64) + "  cactusinv-plan9-mips\n"))
	})
	mux.HandleFunc("/dist/"+asset, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(body) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return selfupdate.DistURL(srv.URL + "/VERSION")
}

func running(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "cactusinv")
	if err := os.WriteFile(exe, script("1.15.0"), 0o755); err != nil { //nolint:gosec // a test executable
		t.Fatal(err)
	}
	return exe
}

func TestInstallSwapsTheBinary(t *testing.T) {
	exe := running(t)
	var last int64
	in := selfupdate.NewFor(publish(t, script("1.16.0"), ""), exe)
	if p := in.CanInstall(); p != "" {
		t.Fatal(p)
	}
	if err := in.Install(context.Background(), "1.16.0", func(done, _ int64) { last = done }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != string(script("1.16.0")) {
		t.Fatalf("in place: %q", b)
	}
	if b, _ := os.ReadFile(selfupdate.OldPath(exe)); string(b) != string(script("1.15.0")) {
		t.Fatalf("old kept: %q", b)
	}
	if last != int64(len(script("1.16.0"))) {
		t.Fatalf("progress ended at %d", last)
	}
}

func TestInstallRefusesABadDownload(t *testing.T) {
	for name, c := range map[string]struct {
		body []byte
		sum  string
		want string
	}{
		"checksum":      {script("1.16.0"), strings.Repeat("a", 64), "does not match"},
		"wrong version": {script("1.15.9"), "", `version "1.15.9", not 1.16.0`},
		"not a program": {[]byte("not a program"), "", "does not run here"},
	} {
		t.Run(name, func(t *testing.T) {
			exe := running(t)
			err := selfupdate.NewFor(publish(t, c.body, c.sum), exe).Install(context.Background(), "1.16.0", nil)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if b, _ := os.ReadFile(exe); string(b) != string(script("1.15.0")) {
				t.Fatal("the running binary was touched")
			}
			if _, err := os.Stat(exe + ".download"); !os.IsNotExist(err) {
				t.Fatal("the download was left behind")
			}
		})
	}
}

func TestCanInstall(t *testing.T) {
	if p := selfupdate.New("https://example.invalid/dist/", true).CanInstall(); !strings.Contains(p, "container") {
		t.Fatalf("container: %q", p)
	}
	if p := selfupdate.NewFor("", running(t)).CanInstall(); !strings.Contains(p, "VERSION file") {
		t.Fatalf("no dist: %q", p)
	}
	if got := selfupdate.DistURL("https://raw.githubusercontent.com/albievan/cactusinv/main/VERSION"); got != "https://raw.githubusercontent.com/albievan/cactusinv/main/dist/" {
		t.Fatal(got)
	}
	if selfupdate.DistURL("https://example.com/latest.txt") != "" || selfupdate.AssetName("windows", "amd64") != "cactusinv-windows-amd64.exe" {
		t.Fatal("names")
	}
	if selfupdate.OldPath(`C:\cactusinv.exe`) != `C:\cactusinv.old.exe` {
		t.Fatal("windows old path")
	}
}
