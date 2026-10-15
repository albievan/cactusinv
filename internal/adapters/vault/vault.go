// Package vault implements ports.Secrets over HashiCorp Vault's KV v2 HTTP API
// (ADR-006). Only two endpoints are needed, so no SDK is used.
package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/albievan/simpleinvoice/internal/ports"
)

// Config locates the secrets.
type Config struct {
	Addr      string // https://vault:8200
	TokenFile string // file holding the token (compose/k8s secret mount)
	KVPath    string // e.g. secret/data/cactusinv
	Client    *http.Client
}

// Client reads one KV v2 secret and serves its keys.
type Client struct {
	cfg   Config
	token string
	mu    sync.RWMutex
	data  map[string]string
}

var _ ports.Secrets = (*Client)(nil)

// New reads the token file and loads the secret.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Addr == "" || cfg.KVPath == "" {
		return nil, errors.New("vault.New: address and KV path are required")
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: 10 * time.Second}
	}
	raw, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		return nil, fmt.Errorf("vault.New: token file: %w", err)
	}
	c := &Client{cfg: cfg, token: strings.TrimSpace(string(raw))}
	if c.token == "" {
		return nil, errors.New("vault.New: token file is empty")
	}
	if err := c.Load(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// Load (re)reads the secret from Vault.
func (c *Client) Load(ctx context.Context) error {
	var body struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/v1/"+strings.TrimPrefix(c.cfg.KVPath, "/"), &body); err != nil {
		return fmt.Errorf("vault.Load: %w", err)
	}
	data := map[string]string{}
	for k, v := range body.Data.Data {
		if s, ok := v.(string); ok {
			data[k] = s
		}
	}
	c.mu.Lock()
	c.data = data
	c.mu.Unlock()
	return nil
}

// Get implements ports.Secrets.
func (c *Client) Get(_ context.Context, key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[key]
	if !ok {
		return "", fmt.Errorf("vault.Get: secret %q not found at %s", key, c.cfg.KVPath)
	}
	return v, nil
}

// Ping checks the token is still valid (readiness).
func (c *Client) Ping(ctx context.Context) error {
	return c.get(ctx, "/v1/auth/token/lookup-self", nil)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.Addr, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", c.token)
	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("vault returned %s", resp.Status)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}
