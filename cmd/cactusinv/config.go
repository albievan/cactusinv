package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/albievan/cactusinv/internal/adapters/filesecrets"
	"github.com/albievan/cactusinv/internal/adapters/release"
	"github.com/albievan/cactusinv/internal/adapters/vault"
	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

type mariadbDB = sql.DB

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
	TrustedProxies       []netip.Prefix
	DefaultAdminUser     string
	DefaultAdminPassword string
	// Deployment: set by deploy.sh so System Settings can show the folders mapped now.
	DeployTarget    string
	HostProjectDir  string
	HostBackupDir   string
	HostInvoicesDir string
	// UpdateURL is the published VERSION file (System Settings → Check for updates);
	// "" when CACTUSINV_UPDATE_URL=off.
	UpdateURL string
	// Container: the image sets CACTUSINV_CONTAINER=1; it never replaces its own binary.
	Container bool
	// Backups (ADR-010).
	BackupDir           string
	BackupTimes         []domain.TimeOfDay
	BackupLocation      *time.Location
	BackupRetentionDays int
	BackupMaxUploadMB   int
	RestoreDBName       string

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
	c.BackupDir = str("CACTUSINV_BACKUP_DIR", "/data/backups")
	c.DeployTarget = getenv("CACTUSINV_DEPLOY_TARGET")
	c.Container = boolean("CACTUSINV_CONTAINER", false) || c.DeployTarget != ""
	c.HostProjectDir = getenv("CACTUSINV_HOST_PROJECT_DIR")
	c.HostBackupDir = getenv("CACTUSINV_HOST_BACKUP_DIR")
	c.HostInvoicesDir = getenv("CACTUSINV_HOST_INVOICES_DIR")
	if c.UpdateURL = str("CACTUSINV_UPDATE_URL", release.DefaultURL); c.UpdateURL == "off" {
		c.UpdateURL = ""
	} else if u, err := url.Parse(c.UpdateURL); err != nil || u.Host == "" || (u.Scheme != "https" && (u.Scheme != "http" || !loopback(u.Hostname()))) {
		// The update is a program the server installs and runs: it comes over HTTPS (plain
		// http only from this machine), so nobody on the way can swap it.
		return config{}, fmt.Errorf("CACTUSINV_UPDATE_URL must be an https URL (http only to localhost) or off, not %q", c.UpdateURL)
	}
	for _, v := range strings.FieldsFunc(getenv("CACTUSINV_TRUSTED_PROXIES"), func(r rune) bool { return r == ',' || r == ' ' }) {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			a, aerr := netip.ParseAddr(v)
			if aerr != nil {
				errs = append(errs, fmt.Errorf("CACTUSINV_TRUSTED_PROXIES: %q is not an address or CIDR", v))
				continue
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}
	c.BackupRetentionDays = integer("CACTUSINV_BACKUP_RETENTION_DAYS", 14)
	c.BackupMaxUploadMB = integer("CACTUSINV_BACKUP_MAX_UPLOAD_MB", 256)
	c.RestoreDBName = str("CACTUSINV_RESTORE_DB_NAME", c.DBName+"_restore")
	if times, err := domain.ParseBackupTimes(str("CACTUSINV_BACKUP_TIMES", "02:00")); err != nil {
		errs = append(errs, fmt.Errorf("CACTUSINV_BACKUP_TIMES: %w", err))
	} else {
		c.BackupTimes = times
	}
	// Scheduled backup times are wall-clock times in TZ (the container's time zone).
	c.BackupLocation = time.UTC
	if tz := getenv("TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			errs = append(errs, fmt.Errorf("TZ: unknown time zone %q", tz))
		} else {
			c.BackupLocation = loc
		}
	}
	if c.BackupMaxUploadMB < 1 || c.BackupMaxUploadMB > 16384 {
		errs = append(errs, errors.New("CACTUSINV_BACKUP_MAX_UPLOAD_MB must be 1-16384"))
	}
	if c.BackupRetentionDays < 0 {
		errs = append(errs, errors.New("CACTUSINV_BACKUP_RETENTION_DAYS must be 0 (keep all) or more"))
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

// loopback reports whether host is this machine.
func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && a.IsLoopback()
}
