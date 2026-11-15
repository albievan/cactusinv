//go:build integration

package mariadb_test

import (
	"context"
	"strings"
	"testing"

	"github.com/albievan/cactusinv/internal/adapters/mariadb"
)

// TestInspector (M57): MariaDB 11's version, connections, size and every variable and
// status value, read through the application's own connection.
func TestInspector(t *testing.T) {
	reset(t)
	st, err := mariadb.Inspector{DB: testDB}.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(st.Version, "11.") || st.Uptime <= 0 || st.MaxConnections <= 0 || st.Connections <= 0 || st.Tables < 20 || st.SizeBytes <= 0 {
		t.Fatalf("status = %+v", st)
	}
	if len(st.Variables) < 100 || len(st.Status) < 100 || st.Charset == "" || st.Pool.Open < 1 {
		t.Fatalf("%d variables, %d status, pool %+v", len(st.Variables), len(st.Status), st.Pool)
	}
	for i := 1; i < len(st.Variables); i++ {
		if st.Variables[i-1].Name > st.Variables[i].Name {
			t.Fatal("variables are not sorted")
		}
	}
}
