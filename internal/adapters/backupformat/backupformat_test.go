package backupformat

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
)

var at = time.Date(2026, 10, 15, 2, 0, 0, 123456000, time.UTC)

func testHeader() domain.BackupHeader {
	return domain.BackupHeader{
		FormatVersion: 1, SchemaVersion: 13, AppVersion: "1.5.0", Build: "1", CreatedAt: at, CreatedBy: "albie",
		Tables: []domain.BackupTable{
			{Name: "currencies", Columns: []domain.BackupColumn{{Name: "code", Kind: domain.ColumnText}, {Name: "minor_units", Kind: domain.ColumnInt}}},
			{Name: "kinds", Columns: []domain.BackupColumn{
				{Name: "i", Kind: domain.ColumnInt},
				{Name: "d", Kind: domain.ColumnDecimal},
				{Name: "t", Kind: domain.ColumnText},
				{Name: "b", Kind: domain.ColumnBytes},
				{Name: "dt", Kind: domain.ColumnDateTime},
				{Name: "dd", Kind: domain.ColumnDate},
			}},
			{Name: "empty", Columns: []domain.BackupColumn{{Name: "x", Kind: domain.ColumnInt}}},
			{Name: "audit_log", Columns: []domain.BackupColumn{{Name: "seq", Kind: domain.ColumnInt}, {Name: "hash", Kind: domain.ColumnText}}},
		},
	}
}

type memSource struct {
	h    domain.BackupHeader
	rows map[string][][]any
}

func (m memSource) SchemaVersion() int64         { return m.h.SchemaVersion }
func (m memSource) Tables() []domain.BackupTable { return m.h.Tables }
func (m memSource) ScanTable(_ context.Context, table string, fn func([]any) error) error {
	for _, r := range m.rows[table] {
		if err := fn(r); err != nil {
			return err
		}
	}
	return nil
}

func testRows() map[string][][]any {
	return map[string][][]any{
		"currencies": {{"ZAR", int64(2)}, {"JPY", int64(0)}},
		"kinds": {
			{int64(-9223372036854775808), "-12.50", "Grüße 🌵 \"quoted\" <tag> & \u2028", []byte{0, 1, 2, 255}, at, "2026-02-28"},
			{int64(9223372036854775807), nil, "", []byte{}, nil, nil},
			{nil, "0", "line\nbreak\ttab", nil, time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC), "9999-12-31"},
		},
		"audit_log": {{int64(1), strings.Repeat("a", 64)}, {int64(2), strings.Repeat("b", 64)}},
	}
}

// sink records what Decode delivers.
type sink struct {
	header domain.BackupHeader
	rows   map[string][][]any
	err    error
}

func (s *sink) Header(_ context.Context, h domain.BackupHeader) error {
	s.header, s.rows = h, map[string][][]any{}
	return s.err
}

func (s *sink) Row(_ context.Context, table string, row []any) error {
	s.rows[table] = append(s.rows[table], row)
	return nil
}

func encode(t testing.TB, c *Codec, rows map[string][][]any) ([]byte, domain.BackupDigest) {
	t.Helper()
	var buf bytes.Buffer
	d, err := c.Encode(context.Background(), &buf, testHeader(), memSource{h: testHeader(), rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), d
}

func TestRoundTrip(t *testing.T) {
	c := New(Limits{PartSize: 64}) // tiny parts: every table spans several
	data, d := encode(t, c, testRows())
	if d.AuditHead.Seq != 2 || len(d.Tables) != 4 || d.Tables[1].Rows != 3 || d.Tables[2].Rows != 0 {
		t.Fatalf("digest = %+v", d)
	}
	var s sink
	h, got, err := c.Decode(context.Background(), bytes.NewReader(data), &s)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, d) || h.SchemaVersion != 13 || !h.CreatedAt.Equal(at) {
		t.Fatalf("digest %+v vs %+v", got, d)
	}
	for table, want := range testRows() {
		if !reflect.DeepEqual(s.rows[table], want) {
			t.Fatalf("%s:\n got %#v\nwant %#v", table, s.rows[table], want)
		}
	}
	// Deterministic: the same snapshot encodes to the same bytes.
	again, _ := encode(t, c, testRows())
	if !bytes.Equal(again, data) {
		t.Fatal("encoding is not deterministic")
	}
	if entries := tarNames(t, data); !reflect.DeepEqual(entries[:3], []string{"header.json", "tables/currencies/000001.jsonl", "tables/kinds/000001.jsonl"}) || entries[len(entries)-1] != "digest.json" {
		t.Fatalf("entries = %v", entries)
	}
}

func tarNames(t testing.TB, data []byte) []string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	var out []string
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, hd.Name)
	}
}

type entry struct {
	name string
	data []byte
	typ  byte
}

// readEntries / writeEntries let tests rebuild a stream with changes (as someone holding
// the key could).
func readEntries(t testing.TB, data []byte) []entry {
	t.Helper()
	gz, _ := gzip.NewReader(bytes.NewReader(data))
	tr := tar.NewReader(gz)
	var out []entry
	for {
		hd, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out = append(out, entry{name: hd.Name, data: b, typ: hd.Typeflag})
	}
}

func writeEntries(t testing.TB, es []entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range es {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hd := &tar.Header{Typeflag: typ, Name: e.name, Mode: 0o600, Size: int64(len(e.data)), Format: tar.FormatPAX}
		if typ != tar.TypeReg {
			hd.Size, hd.Linkname = 0, "/etc/passwd"
		}
		if err := tw.WriteHeader(hd); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			_, _ = tw.Write(e.data)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestDecodeRefusals(t *testing.T) {
	c := New(Limits{})
	data, _ := encode(t, c, testRows())
	base := readEntries(t, data)
	edit := func(f func([]entry) []entry) []byte {
		es := make([]entry, len(base))
		for i, e := range base {
			es[i] = entry{name: e.name, data: bytes.Clone(e.data)}
		}
		return writeEntries(t, f(es))
	}
	find := func(es []entry, name string) int {
		for i, e := range es {
			if e.name == name {
				return i
			}
		}
		t.Fatalf("no %s", name)
		return -1
	}
	cases := map[string][]byte{
		"changed row (digest mismatch)": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].data = bytes.Replace(es[i].data, []byte(`"ZAR",2`), []byte(`"ZAR",3`), 1)
			return es
		}),
		"dropped row": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].data = []byte("[\"ZAR\",2]\n")
			return es
		}),
		"non-canonical row": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].data = bytes.Replace(es[i].data, []byte(`["ZAR",2]`), []byte(`[ "ZAR", 2 ]`), 1)
			return es
		}),
		"wrong type": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].data = bytes.Replace(es[i].data, []byte(`"ZAR",2`), []byte(`"ZAR","2"`), 1)
			return es
		}),
		"no trailing newline": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].data = bytes.TrimSuffix(es[i].data, []byte("\n"))
			return es
		}),
		"missing digest": edit(func(es []entry) []entry { return es[:len(es)-1] }),
		"header not first": edit(func(es []entry) []entry {
			es[0], es[1] = es[1], es[0]
			return es
		}),
		"tables out of order": edit(func(es []entry) []entry {
			es[1], es[2] = es[2], es[1]
			return es
		}),
		"entry after digest": edit(func(es []entry) []entry { return append(es, entry{name: "extra", data: []byte("x")}) }),
		"traversal name": edit(func(es []entry) []entry {
			return append(es[:1], append([]entry{{name: "../../etc/cron.d/x", data: []byte("x")}}, es[1:]...)...)
		}),
		"table not in header": edit(func(es []entry) []entry {
			return append(es[:1], append([]entry{{name: "tables/users/000001.jsonl", data: []byte("[\"x\"]\n")}}, es[1:]...)...)
		}),
		"part gap": edit(func(es []entry) []entry {
			i := find(es, "tables/currencies/000001.jsonl")
			es[i].name = "tables/currencies/000002.jsonl"
			return es
		}),
		"symlink": edit(func(es []entry) []entry {
			return append(es[:1], append([]entry{{name: "tables/currencies/000001.jsonl", typ: tar.TypeSymlink}}, es[1:]...)...)
		}),
		"header unknown field": edit(func(es []entry) []entry {
			es[0].data = bytes.Replace(es[0].data, []byte(`"build"`), []byte(`"x":1,"build"`), 1)
			return es
		}),
		"digest garbage": edit(func(es []entry) []entry {
			es[len(es)-1].data = []byte("{")
			return es
		}),
		"data after the stream": append(bytes.Clone(data), 0x1f, 0x8b, 0, 0),
		"not gzip":              []byte("-- MariaDB dump"),
		"empty":                 nil,
		"truncated":             data[:len(data)/2],
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := c.Decode(context.Background(), bytes.NewReader(in), &sink{})
			if !errors.Is(err, domain.ErrBackupInvalid) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	future := edit(func(es []entry) []entry {
		es[0].data = []byte(`{"formatVersion":2}`)
		return es
	})
	if _, _, err := c.Decode(context.Background(), bytes.NewReader(future), &sink{}); !errors.Is(err, domain.ErrBackupUnsupported) {
		t.Fatalf("future format: %v", err)
	}
	refuse := errors.New("too new for me")
	if _, _, err := c.Decode(context.Background(), bytes.NewReader(data), &sink{err: refuse}); !errors.Is(err, refuse) {
		t.Fatalf("the sink's refusal is returned as is: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Decode(ctx, bytes.NewReader(data), &sink{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}

// TestLimits: a decompression bomb and an oversized row are refused without reading them
// into memory.
func TestLimits(t *testing.T) {
	c := New(Limits{MaxTotal: 1 << 20, MaxRow: 1 << 10})
	hdr, _ := json.Marshal(testHeader())
	bomb := writeEntries(t, []entry{{name: "header.json", data: hdr}, {name: "tables/currencies/000001.jsonl", data: make([]byte, 8<<20)}})
	if len(bomb) > 64<<10 {
		t.Fatalf("bomb is %d bytes compressed", len(bomb))
	}
	if _, _, err := c.Decode(context.Background(), bytes.NewReader(bomb), &sink{}); !errors.Is(err, domain.ErrBackupInvalid) {
		t.Fatalf("bomb: %v", err)
	}
	long := []byte(`["` + strings.Repeat("x", 4<<10) + `",1]` + "\n")
	big := writeEntries(t, []entry{{name: "header.json", data: hdr}, {name: "tables/currencies/000001.jsonl", data: long}})
	if _, _, err := c.Decode(context.Background(), bytes.NewReader(big), &sink{}); !errors.Is(err, domain.ErrBackupInvalid) || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("long row: %v", err)
	}
	if _, err := New(Limits{MaxRow: 8}).Encode(context.Background(), io.Discard, testHeader(), memSource{h: testHeader(), rows: testRows()}); err == nil {
		t.Fatal("encode refuses rows above the limit")
	}
}

func TestEncodeRefusesBadInput(t *testing.T) {
	c := New(Limits{})
	bad := testHeader()
	bad.Tables[0].Name = "Bad"
	if _, err := c.Encode(context.Background(), io.Discard, bad, memSource{h: bad}); !errors.Is(err, domain.ErrBackupInvalid) {
		t.Fatalf("header: %v", err)
	}
	for name, rows := range map[string][][]any{
		"bad utf8":   {{"\xff", int64(1)}},
		"wrong type": {{"ZAR", 2}},
		"short":      {{"ZAR"}},
	} {
		if _, err := c.Encode(context.Background(), io.Discard, testHeader(), memSource{h: testHeader(), rows: map[string][][]any{"currencies": rows}}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	scanErr := errors.New("connection lost")
	if _, err := c.Encode(context.Background(), io.Discard, testHeader(), failing{memSource{h: testHeader()}, scanErr}); !errors.Is(err, scanErr) {
		t.Fatalf("scan error: %v", err)
	}
}

type failing struct {
	memSource
	err error
}

func (f failing) ScanTable(context.Context, string, func([]any) error) error { return f.err }

func FuzzDecode(f *testing.F) {
	c := New(Limits{MaxTotal: 1 << 20, MaxRow: 1 << 12, MaxHeader: 1 << 14})
	data, _ := encode(f, New(Limits{PartSize: 64}), testRows())
	f.Add(data)
	f.Add(writeEntries(f, readEntries(f, data)[:2]))
	f.Add([]byte{0x1f, 0x8b})
	f.Fuzz(func(t *testing.T, in []byte) {
		s := &sink{}
		_, _, err := c.Decode(context.Background(), bytes.NewReader(in), s)
		if err == nil {
			return
		}
		if !errors.Is(err, domain.ErrBackupInvalid) && !errors.Is(err, domain.ErrBackupUnsupported) {
			t.Fatalf("unexpected error kind: %v", err)
		}
	})
}

// FuzzRowRoundTrip: any decodable row re-encodes to the same bytes.
func FuzzRowRoundTrip(f *testing.F) {
	tbl := testHeader().Tables[1]
	for _, r := range testRows()["kinds"] {
		line, _ := encodeRow(tbl, r)
		f.Add(line[:len(line)-1])
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		row, err := decodeRow(tbl, line)
		if err != nil {
			return
		}
		again, err := encodeRow(tbl, row)
		if err != nil || !bytes.Equal(again[:len(again)-1], line) {
			t.Fatalf("%q decoded but re-encodes as %q (%v)", line, again, err)
		}
		if fmt.Sprint(row) == "" {
			t.Fatal("unreachable")
		}
	})
}
