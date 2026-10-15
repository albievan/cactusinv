package archive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSave(t *testing.T) {
	root := t.TempDir()
	d := Dir{Root: root}
	ctx := context.Background()
	if err := d.Save(ctx, "2026/Inv_no_CD_1195_26.pdf", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := d.Save(ctx, "2026/Inv_no_CD_1195_26.pdf", []byte("v2")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "2026", "Inv_no_CD_1195_26.pdf"))
	if err != nil || string(got) != "v2" {
		t.Fatalf("got %q %v", got, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "2026"))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
	for _, bad := range []string{"../x.pdf", "/etc/x", ".", ""} {
		if err := d.Save(ctx, bad, nil); err == nil {
			t.Errorf("Save(%q) should fail", bad)
		}
	}
	if err := d.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := (Dir{Root: filepath.Join(root, "missing")}).Ping(ctx); err == nil {
		t.Fatal("ping missing dir")
	}
}
