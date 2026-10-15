package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/albievan/cactusinve/internal/adapters/filesecrets"
	"github.com/albievan/cactusinve/internal/adapters/vault"
	"github.com/albievan/cactusinve/internal/ports"
)

type (
	mariadbDB = sql.DB
	portsMail = ports.Mail
)

// config is every setting, read from the environment only (12-factor). Secrets are not
// here: they come from files (Docker secrets) or Vault.
type config struct {
	HTTPAddr             string
	LogLevel             string
	DBHost               string
	DBPort               int
	DBName               string
	DBUser               string
	DBWait               time.Duration
	SecretsSource        string // file | vault
	SecretsDir           string
	VaultAddr            string
	VaultTokenFile       string
	VaultKVPath          string
	SMTPHost             string
	SMTPPort             int
	SMTPUser             string
	SMTPFrom             string
	SMTPTLS              string
	InvoicesDir          string
	Workers              bool
	MigrateOnStart       bool
	CookieSecure         bool
	DefaultAdminUser     string
	DefaultAdminPassword string

	secrets ports.Secrets
}

func loadConfig(getenv func(string) string) (config, error) {
	str := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	var errs []error
	integer := func(k string, def int) int {
		v := getenv(k)
		if v == "" {
			return def
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: not a number", k))
		}
		return n
	}
	boolean := func(k string, def bool) bool {
		v := getenv(k)
		if v == "" {
			return def
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: not a boolean", k))
		}
		return b
	}
	c := config{
		HTTPAddr:             str("CACTUSINV_HTTP_ADDR", ":8080"),
		LogLevel:             str("CACTUSINV_LOG_LEVEL", "info"),
		DBHost:               str("CACTUSINV_DB_HOST", "localhost"),
		DBPort:               integer("CACTUSINV_DB_PORT", 3306),
		DBName:               str("CACTUSINV_DB_NAME", "cactusinv"),
		DBUser:               str("CACTUSINV_DB_USER", "cactusinv"),
		DBWait:               time.Duration(integer("CACTUSINV_DB_WAIT_SECONDS", 60)) * time.Second,
		SecretsSource:        str("CACTUSINV_SECRETS", "file"),
		SecretsDir:           str("CACTUSINV_SECRETS_DIR", "/run/secrets"),
		VaultAddr:            getenv("CACTUSINV_VAULT_ADDR"),
		VaultTokenFile:       str("CACTUSINV_VAULT_TOKEN_FILE", "/run/secrets/vault_token"),
		VaultKVPath:          str("CACTUSINV_VAULT_KV_PATH", "secret/data/cactusinv"),
		SMTPHost:             getenv("CACTUSINV_SMTP_HOST"),
		SMTPPort:             integer("CACTUSINV_SMTP_PORT", 587),
		SMTPUser:             getenv("CACTUSINV_SMTP_USER"),
		SMTPFrom:             str("CACTUSINV_SMTP_FROM", "cactusInv <invoices@localhost>"),
		SMTPTLS:              str("CACTUSINV_SMTP_TLS", "starttls"),
		InvoicesDir:          str("CACTUSINV_INVOICES_DIR", "/data/invoices"),
		Workers:              boolean("CACTUSINV_WORKERS", true),
		MigrateOnStart:       boolean("CACTUSINV_MIGRATE_ON_START", true),
		CookieSecure:         boolean("CACTUSINV_COOKIE_SECURE", true),
		DefaultAdminUser:     str("CACTUSINV_DEFAULT_ADMIN_USER", "admin"),
		DefaultAdminPassword: str("CACTUSINV_DEFAULT_ADMIN_PASSWORD", "admin"),
	}
	switch c.SecretsSource {
	case "file":
		c.secrets = filesecrets.Dir{Root: c.SecretsDir}
	case "vault":
		if c.VaultAddr == "" {
			errs = append(errs, errors.New("CACTUSINV_VAULT_ADDR is required when CACTUSINV_SECRETS=vault"))
		}
	default:
		errs = append(errs, errors.New("CACTUSINV_SECRETS must be file or vault"))
	}
	return c, errors.Join(errs...)
}

func (c *config) store(ctx context.Context) (ports.Secrets, error) {
	if c.secrets != nil {
		return c.secrets, nil
	}
	v, err := vault.New(ctx, vault.Config{Addr: c.VaultAddr, TokenFile: c.VaultTokenFile, KVPath: c.VaultKVPath})
	if err != nil {
		return nil, err
	}
	c.secrets = v
	return v, nil
}

func (c *config) secret(ctx context.Context, key string) (string, error) {
	s, err := c.store(ctx)
	if err != nil {
		return "", err
	}
	return s.Get(ctx, key)
}

// optionalSecret returns "" when the secret is absent (e.g. SMTP without auth).
func (c *config) optionalSecret(ctx context.Context, key string) (string, error) {
	v, err := c.secret(ctx, key)
	if err != nil && c.SecretsSource == "file" {
		if _, statErr := os.Stat(c.SecretsDir + "/" + key); os.IsNotExist(statErr) {
			return "", nil
		}
	}
	return v, err
}
