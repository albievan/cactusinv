// Package release reads the newest published version of cactusInv (System Settings →
// Check for updates; M27): a plain-text VERSION file, by default the one on the main
// branch on GitHub, which is what ./deploy.sh --upgrade installs.
package release

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultURL is the published VERSION file.
const DefaultURL = "https://raw.githubusercontent.com/albievan/cactusinv/main/VERSION"

// Source implements ports.ReleaseSource over HTTP(S).
type Source struct {
	url    string
	client *http.Client
}

// New returns a Source reading url.
func New(url string) *Source {
	return &Source{url: url, client: &http.Client{Timeout: 10 * time.Second}}
}

// LatestVersion implements ports.ReleaseSource: the file's first line, as published.
func (s *Source) LatestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", fmt.Errorf("release.LatestVersion: %w", err)
	}
	req.Header.Set("User-Agent", "cactusinv")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("release.LatestVersion: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release.LatestVersion: %s", resp.Status)
	}
	// A VERSION file is a few bytes; never read more than a line's worth.
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", fmt.Errorf("release.LatestVersion: %w", err)
	}
	first, _, _ := strings.Cut(string(b), "\n")
	return strings.TrimSpace(first), nil
}
