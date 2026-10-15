package filesecrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGet(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "db_password"), []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Dir{Root: root}
	if v, err := d.Get(context.Background(), "db_password"); err != nil || v != "s3cret" {
		t.Fatalf("Get = %q %v", v, err)
	}
	for _, bad := range []string{"missing", "../etc", "", ".hidden"} {
		if _, err := d.Get(context.Background(), bad); err == nil {
			t.Errorf("Get(%q) should fail", bad)
		}
	}
}
