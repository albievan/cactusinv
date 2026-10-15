package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfig(t *testing.T) {
	c, err := loadConfig(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.HTTPAddr != ":8080" || c.DefaultAdminUser != "admin" || c.DefaultAdminPassword != "admin" || !c.MigrateOnStart || c.InvoicesDir != "/data/invoices" {
		t.Fatalf("defaults = %+v", c)
	}
	for _, bad := range []map[string]string{
		{"CACTUSINV_DB_PORT": "x"},
		{"CACTUSINV_WORKERS": "maybe"},
		{"CACTUSINV_SECRETS": "env"},
		{"CACTUSINV_SECRETS": "vault"},
	} {
		if _, err := loadConfig(env(bad)); err == nil {
			t.Errorf("loadConfig(%v) should fail", bad)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "db_password"), []byte("pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ = loadConfig(env(map[string]string{"CACTUSINV_SECRETS_DIR": dir}))
	ctx := context.Background()
	if v, err := c.secret(ctx, "db_password"); err != nil || v != "pw" {
		t.Fatalf("secret = %q %v", v, err)
	}
	if v, err := c.optionalSecret(ctx, "smtp_password"); err != nil || v != "" {
		t.Fatalf("optional secret = %q %v", v, err)
	}
}
