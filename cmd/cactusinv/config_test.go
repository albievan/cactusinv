package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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
	if c.BackupDir != "/data/backups" || len(c.BackupTimes) != 1 || c.BackupTimes[0].Hour != 2 || c.BackupRetentionDays != 14 ||
		c.BackupMaxUploadMB != 256 || c.RestoreDBName != "cactusinv_restore" || c.BackupLocation != time.UTC {
		t.Fatalf("backup defaults = %+v", c)
	}
	c, err = loadConfig(env(map[string]string{"CACTUSINV_DB_NAME": "books", "CACTUSINV_BACKUP_TIMES": "14:00, 02:30", "TZ": "Africa/Johannesburg"}))
	if err != nil || c.RestoreDBName != "books_restore" || len(c.BackupTimes) != 2 || c.BackupLocation.String() != "Africa/Johannesburg" {
		t.Fatalf("backup settings = %+v %v", c, err)
	}
	for _, bad := range []map[string]string{
		{"CACTUSINV_DB_PORT": "x"},
		{"CACTUSINV_WORKERS": "maybe"},
		{"CACTUSINV_SECRETS": "env"},
		{"CACTUSINV_SECRETS": "vault"},
		{"CACTUSINV_BACKUP_TIMES": "2am"},
		{"TZ": "Mars/Olympus"},
		{"CACTUSINV_BACKUP_MAX_UPLOAD_MB": "0"},
		{"CACTUSINV_BACKUP_RETENTION_DAYS": "-1"},
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
