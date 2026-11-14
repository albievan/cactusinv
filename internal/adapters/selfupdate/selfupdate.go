// Package selfupdate replaces a standalone cactusInv binary with a published release
// (M31): the binaries and their SHA256SUMS are published in dist/ on the
// main branch beside VERSION. A container never replaces itself (its image would bring the
// old version back at the next start); it is updated with ./deploy.sh --upgrade.
package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// maxBinary bounds a download; a release is about 20 MB.
const maxBinary = 256 << 20

// AssetName is the published file for an OS and CPU, e.g. cactusinv-linux-amd64.
func AssetName(goos, goarch string) string {
	name := "cactusinv-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// gitHubVersion is the VERSION file on GitHub's main branch (release.DefaultURL); its
// releases are published as GitHub Releases (M48), not committed beside it.
const (
	gitHubVersion  = "https://raw.githubusercontent.com/albievan/cactusinv/main/VERSION"
	gitHubReleases = "https://github.com/albievan/cactusinv/releases/download/v{version}/"
)

// DistURL is where the files of a release are published for a VERSION file URL: for the
// one on GitHub, that release's GitHub Release (with {version} in it); for any other, the
// dist/ folder beside it (a self-hosted mirror). "" when the URL names no VERSION file, so
// there is nothing to install from.
func DistURL(versionURL string) string {
	if versionURL == gitHubVersion {
		return gitHubReleases
	}
	base, ok := strings.CutSuffix(versionURL, "/VERSION")
	if !ok || base == "" {
		return ""
	}
	return base + "/dist/"
}

// versionRe is a release version as published ("1.27.0"): nothing else goes in a URL.
var versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Installer implements ports.Upgrader for the running binary.
type Installer struct {
	dist      string
	container bool
	client    *http.Client
	base      string // dist with the version being installed filled in (Install)
	exe       string // the running binary, symlinks resolved ("" when unknown)
	exeErr    error
	key       ed25519.PublicKey // checks SHA256SUMS.sig (releasePublicKey)
}

// New returns an Installer downloading from dist (see DistURL). container marks an
// installation that must not replace itself.
func New(dist string, container bool) *Installer {
	key, _ := base64.StdEncoding.DecodeString(releasePublicKey)
	i := &Installer{dist: dist, container: container, client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: noDowngrade}, key: key}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	i.exe, i.exeErr = exe, err
	return i
}

// Executable is the binary an install replaces.
func (i *Installer) Executable() string { return i.exe }

// CanInstall implements ports.Upgrader.
func (i *Installer) CanInstall() string {
	switch {
	case i.container:
		return "This cactusInv runs in a container: a new version would only last until the container restarts. Update the image instead with ./deploy.sh --upgrade."
	case i.dist == "":
		return "CACTUSINV_UPDATE_URL does not name a VERSION file with a dist/ folder beside it, so there is nothing to install from."
	case i.exeErr != nil:
		return "The running program's file could not be found: " + i.exeErr.Error()
	}
	f, err := os.CreateTemp(filepath.Dir(i.exe), ".cactusinv-write-test-*")
	if err != nil {
		return "cactusInv cannot write to its folder (" + filepath.Dir(i.exe) + "), so it cannot replace itself. Give the user it runs as write access, or update it by hand."
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return ""
}

// Install implements ports.Upgrader: download, check the SHA-256 and the version the new
// binary reports, then swap it in (the old one stays as <exe>.old until the next install).
func (i *Installer) Install(ctx context.Context, version string, progress func(done, total int64)) error {
	if p := i.CanInstall(); p != "" {
		return errors.New(p)
	}
	if !versionRe.MatchString(version) {
		return fmt.Errorf("selfupdate: %q is not a release version", version)
	}
	i.base = strings.ReplaceAll(i.dist, "{version}", version)
	asset := AssetName(runtime.GOOS, runtime.GOARCH)
	sums, err := i.checksums(ctx)
	if err != nil {
		return err
	}
	want, ok := sums[asset]
	if !ok {
		return fmt.Errorf("selfupdate: no %s in the published SHA256SUMS: this OS or CPU is not published", asset)
	}
	tmp := i.exe + ".download"
	if err := i.download(ctx, asset, tmp, want, progress); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := checkVersion(ctx, tmp, version); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := putInPlace(i.exe, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (i *Installer) get(ctx context.Context, name string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.base+name, nil)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %w", err)
	}
	req.Header.Set("User-Agent", "cactusinv")
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("selfupdate: %s: %s", name, resp.Status)
	}
	return resp, nil
}

// checksums reads SHA256SUMS (sha256sum format) after checking its signature,
// SHA256SUMS.sig, against the release key (M45). Unlike a missing binary, a missing or
// wrong signature or list is an error: nothing is installed unchecked.
func (i *Installer) checksums(ctx context.Context) (map[string]string, error) {
	sums, err := i.read(ctx, "SHA256SUMS", 64<<10)
	if err != nil {
		return nil, err
	}
	sigText, err := i.read(ctx, "SHA256SUMS.sig", 1<<10)
	if err != nil {
		return nil, fmt.Errorf("selfupdate: the release is not signed: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(i.key) != ed25519.PublicKeySize || !ed25519.Verify(i.key, sums, sig) {
		return nil, errors.New("selfupdate: the release's signature does not match the release key: it was not published by the owner, so it is not installed")
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == sha256.Size*2 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out, sc.Err()
}

// read fetches a small published file whole (at most max bytes).
func (i *Installer) read(ctx context.Context, name string, max int64) ([]byte, error) {
	resp, err := i.get(ctx, name)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: %s: %w", name, err)
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("selfupdate: %s is larger than %d bytes", name, max)
	}
	return b, nil
}

func (i *Installer) download(ctx context.Context, asset, tmp, want string, progress func(done, total int64)) error {
	resp, err := i.get(ctx, asset)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxBinary {
		return fmt.Errorf("selfupdate: %s is %d bytes, more than %d", asset, resp.ContentLength, maxBinary)
	}
	// A file left by an earlier attempt (or planted there) is removed, not written through.
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("selfupdate: %w", err)
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755) //nolint:gosec // an executable must be executable
	if err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	h := sha256.New()
	w := &progressWriter{w: io.MultiWriter(f, h), total: resp.ContentLength, report: progress}
	n, err := io.Copy(w, io.LimitReader(resp.Body, maxBinary+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return fmt.Errorf("selfupdate: downloading %s: %w", asset, err)
	case n > maxBinary:
		return fmt.Errorf("selfupdate: %s is larger than %d bytes", asset, maxBinary)
	case resp.ContentLength >= 0 && n != resp.ContentLength:
		return fmt.Errorf("selfupdate: %s: got %d of %d bytes", asset, n, resp.ContentLength)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("selfupdate: %s: SHA-256 %s does not match the published %s", asset, got, want)
	}
	return nil
}

type progressWriter struct {
	w           io.Writer
	done, total int64
	last        time.Time
	report      func(done, total int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.report != nil && (time.Since(p.last) > 200*time.Millisecond || p.done == p.total) {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

// checkVersion runs the downloaded binary's "version" command: it must start and be the
// version that was asked for, or the running one is left alone.
func checkVersion(ctx context.Context, bin, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output() //nolint:gosec // our own download, checked against SHA256SUMS
	if err != nil {
		return fmt.Errorf("selfupdate: the downloaded program does not run here: %w", err)
	}
	got, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if got != want {
		return fmt.Errorf("selfupdate: the downloaded program is version %q, not %s", got, want)
	}
	return nil
}

// OldPath is where the replaced binary is kept.
func OldPath(exe string) string {
	if base, ok := strings.CutSuffix(exe, ".exe"); ok {
		return base + ".old.exe"
	}
	return exe + ".old"
}

// putInPlace moves the running binary aside and the new one into its place (renames work
// on a running binary on Linux, macOS and Windows); if the second rename fails the old
// binary is put back.
func putInPlace(exe, tmp string) error {
	old := OldPath(exe)
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("selfupdate: moving the running program aside: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("selfupdate: %w; and putting the old program back failed: %w (it is %s)", err, rerr, old)
		}
		return fmt.Errorf("selfupdate: putting the new program in place: %w", err)
	}
	return nil
}

// Restart replaces this process with exe started with the same arguments and environment
// (Unix: exec, same PID, so systemd and logs stay attached; Windows: a new process, and
// this one exits). If the new binary does not start, the old one is put back and started.
func Restart(exe string) error {
	err := restart(exe)
	old := OldPath(exe)
	if _, serr := os.Stat(old); serr != nil {
		return err
	}
	_ = os.Rename(exe, exe+".failed")
	if rerr := os.Rename(old, exe); rerr != nil {
		return fmt.Errorf("%w; rolling back failed: %w", err, rerr)
	}
	if rerr := restart(exe); rerr != nil {
		return fmt.Errorf("%w; the old version did not start either: %w", err, rerr)
	}
	return nil
}

// noDowngrade follows a redirect only if it does not leave HTTPS: the download is a
// program the server will run.
func noDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("selfupdate: too many redirects")
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("selfupdate: refused a redirect from https to %s", req.URL.Scheme)
	}
	return nil
}
