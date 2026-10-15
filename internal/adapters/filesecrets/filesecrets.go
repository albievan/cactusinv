// Package filesecrets implements ports.Secrets with one file per secret, the layout of
// Docker/Compose secrets (/run/secrets/<name>). It is the default for the Docker
// deployment; Vault is the alternative (ADR-006).
package filesecrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/albievan/simpleinvoice/internal/ports"
)

// Dir reads secrets from files in Root.
type Dir struct {
	Root string
}

var _ ports.Secrets = Dir{}

// Get implements ports.Secrets; surrounding whitespace is trimmed.
func (d Dir) Get(_ context.Context, key string) (string, error) {
	if key == "" || strings.ContainsAny(key, `/\`) || strings.HasPrefix(key, ".") {
		return "", fmt.Errorf("filesecrets.Get: invalid key %q", key)
	}
	b, err := os.ReadFile(filepath.Join(d.Root, key))
	if err != nil {
		return "", fmt.Errorf("filesecrets.Get: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}
