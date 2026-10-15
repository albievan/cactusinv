// Package backupformat implements ports.BackupCodec: the cactusInv backup format v1
// (ADR-010), a gzip-compressed tar stream of
//
//	header.json                  the domain.BackupHeader
//	tables/<table>/000001.jsonl  rows, one canonical JSON array per line, in header order;
//	tables/<table>/000002.jsonl  a table is split into parts of at most PartSize bytes
//	digest.json                  rows and SHA-256 of every table, and the audit head
//
// Encryption is not this package's concern (see agecrypt). Decoding treats the input as
// hostile: entry names, order, sizes, every value and the digest are checked, and the
// decompressed size is bounded.
package backupformat

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/albievan/cactusinv/internal/domain"
	"github.com/albievan/cactusinv/internal/ports"
)

// Entry names.
const (
	headerName = "header.json"
	digestName = "digest.json"
)

// DateTimeLayout is how datetime values are written (UTC, microseconds).
const DateTimeLayout = "2006-01-02T15:04:05.000000Z"

// Limits bound what Encode writes and Decode accepts.
type Limits struct {
	PartSize  int   // bytes of rows per table part written by Encode
	MaxHeader int64 // header.json and digest.json
	MaxRow    int   // one encoded row
	MaxTotal  int64 // decompressed bytes of the whole stream
}

// DefaultLimits: 8 MiB parts, 1 MiB header, 32 MiB rows (a MEDIUMBLOB in base64 fits),
// 4 GiB in total.
var DefaultLimits = Limits{PartSize: 8 << 20, MaxHeader: 1 << 20, MaxRow: 32 << 20, MaxTotal: 4 << 30}

// Codec reads and writes the backup format.
type Codec struct {
	limits Limits
}

// New returns a Codec with the given limits (zero fields take DefaultLimits).
func New(l Limits) *Codec {
	if l.PartSize <= 0 {
		l.PartSize = DefaultLimits.PartSize
	}
	if l.MaxHeader <= 0 {
		l.MaxHeader = DefaultLimits.MaxHeader
	}
	if l.MaxRow <= 0 {
		l.MaxRow = DefaultLimits.MaxRow
	}
	if l.MaxTotal <= 0 {
		l.MaxTotal = DefaultLimits.MaxTotal
	}
	return &Codec{limits: l}
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("backupformat: %w: %s", domain.ErrBackupInvalid, fmt.Sprintf(format, args...))
}

// ---- values ----

// appendValue appends the canonical JSON of v (of kind k) to b.
func appendValue(b []byte, k domain.ColumnKind, v any) ([]byte, error) {
	if v == nil {
		return append(b, "null"...), nil
	}
	if err := k.CheckValue(v); err != nil {
		return nil, err
	}
	switch k {
	case domain.ColumnInt:
		return strconv.AppendInt(b, v.(int64), 10), nil
	case domain.ColumnBytes:
		return strconv.AppendQuote(b, base64.StdEncoding.EncodeToString(v.([]byte))), nil
	case domain.ColumnDateTime:
		return strconv.AppendQuote(b, v.(time.Time).Format(DateTimeLayout)), nil
	default: // text, decimal, date: JSON strings
		s, err := json.Marshal(v.(string))
		if err != nil {
			return nil, err
		}
		return append(b, s...), nil
	}
}

// encodeRow returns the canonical line (with its newline) for row.
func encodeRow(t domain.BackupTable, row []any) ([]byte, error) {
	if len(row) != len(t.Columns) {
		return nil, invalid("%s row has %d values, want %d", t.Name, len(row), len(t.Columns))
	}
	b := []byte{'['}
	for i, c := range t.Columns {
		if i > 0 {
			b = append(b, ',')
		}
		var err error
		if b, err = appendValue(b, c.Kind, row[i]); err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name, c.Name, err)
		}
	}
	return append(b, ']', '\n'), nil
}

func decodeValue(k domain.ColumnKind, raw json.RawMessage) (any, error) {
	if string(raw) == "null" {
		return nil, nil
	}
	if k == domain.ColumnInt {
		n, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil {
			return nil, invalid("not an integer")
		}
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, invalid("not a string")
	}
	switch k {
	case domain.ColumnBytes:
		b, err := base64.StdEncoding.Strict().DecodeString(s)
		if err != nil {
			return nil, invalid("not base64")
		}
		return b, nil
	case domain.ColumnDateTime:
		t, err := time.Parse(DateTimeLayout, s)
		if err != nil {
			return nil, invalid("not a datetime")
		}
		return t.UTC(), nil
	}
	return s, nil
}

// decodeRow parses one line; it must be exactly the canonical encoding of its values.
func decodeRow(t domain.BackupTable, line []byte) ([]any, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(line, &raws); err != nil {
		return nil, invalid("%s: a row is not a JSON array", t.Name)
	}
	if len(raws) != len(t.Columns) {
		return nil, invalid("%s: a row has %d values, want %d", t.Name, len(raws), len(t.Columns))
	}
	row := make([]any, len(raws))
	for i, c := range t.Columns {
		v, err := decodeValue(c.Kind, raws[i])
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", t.Name, c.Name, err)
		}
		row[i] = v
	}
	canon, err := encodeRow(t, row) // errors are domain.ErrBackupInvalid
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canon[:len(canon)-1], line) {
		return nil, invalid("%s: a row is not in canonical form", t.Name)
	}
	return row, nil
}

// ---- encode ----

type encoder struct {
	tw    *tar.Writer
	mod   time.Time
	limit int
}

func (e encoder) entry(name string, data []byte) error {
	if err := e.tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: e.mod, Format: tar.FormatPAX,
	}); err != nil {
		return err
	}
	_, err := e.tw.Write(data)
	return err
}

// tableDigest accumulates one table's digest.
type tableDigest struct {
	h    hash.Hash
	rows int64
}

func newTableDigest() *tableDigest { return &tableDigest{h: sha256.New()} }

func (d *tableDigest) add(line []byte) {
	d.h.Write(line)
	d.rows++
}

func (d *tableDigest) result(name string) domain.TableDigest {
	return domain.TableDigest{Name: name, Rows: d.rows, SHA256: hex.EncodeToString(d.h.Sum(nil))}
}

// Encode implements ports.BackupCodec.
func (c *Codec) Encode(ctx context.Context, w io.Writer, h domain.BackupHeader, src ports.BackupSource) (domain.BackupDigest, error) {
	if err := h.Validate(); err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	enc := encoder{tw: tw, mod: h.CreatedAt.UTC().Truncate(time.Second), limit: c.limits.PartSize}
	hdr, err := json.Marshal(h)
	if err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	if err := enc.entry(headerName, hdr); err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	var d domain.BackupDigest
	for _, t := range h.Tables {
		td, err := c.encodeTable(ctx, enc, t, src, &d.AuditHead)
		if err != nil {
			return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
		}
		d.Tables = append(d.Tables, td)
	}
	dg, err := json.Marshal(d)
	if err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	if err := enc.entry(digestName, dg); err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	if err := tw.Close(); err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	if err := gz.Close(); err != nil {
		return domain.BackupDigest{}, fmt.Errorf("backupformat.Encode: %w", err)
	}
	return d, nil
}

func partName(table string, n int) string { return fmt.Sprintf("tables/%s/%06d.jsonl", table, n) }

func (c *Codec) encodeTable(ctx context.Context, enc encoder, t domain.BackupTable, src ports.BackupSource, head *domain.AuditRef) (domain.TableDigest, error) {
	td := newTableDigest()
	var part []byte
	parts := 0
	flush := func() error {
		if len(part) == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		parts++
		err := enc.entry(partName(t.Name, parts), part)
		part = part[:0]
		return err
	}
	err := src.ScanTable(ctx, t.Name, func(row []any) error {
		line, err := encodeRow(t, row)
		if err != nil {
			return err
		}
		if len(line) > c.limits.MaxRow+1 {
			return fmt.Errorf("%s: a row of %d bytes is larger than the limit", t.Name, len(line))
		}
		if len(part) > 0 && len(part)+len(line) > enc.limit {
			if err := flush(); err != nil {
				return err
			}
		}
		part = append(part, line...)
		td.add(line)
		if ref, ok := domain.AuditHeadOf(t, row); ok {
			*head = ref
		}
		return nil
	})
	if err != nil {
		return domain.TableDigest{}, err
	}
	return td.result(t.Name), flush()
}

// ---- decode ----

// countingReader fails once more than max bytes were read (a decompression bomb).
type countingReader struct {
	r   io.Reader
	n   int64
	max int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	if c.n > c.max {
		return n, invalid("larger than %d bytes uncompressed", c.max)
	}
	return n, err
}

var partRe = regexp.MustCompile(`^tables/([a-z][a-z0-9_]{0,63})/([0-9]{6})\.jsonl$`)

// readSmall reads a whole small entry.
func readSmall(tr *tar.Reader, hd *tar.Header, max int64) ([]byte, error) {
	if hd.Size < 0 || hd.Size > max {
		return nil, invalid("%s is too large", hd.Name)
	}
	data, err := io.ReadAll(io.LimitReader(tr, hd.Size))
	if err != nil {
		return nil, invalidRead(err)
	}
	return data, nil
}

// invalidRead keeps a decompression-limit error and reports anything else as damage.
func invalidRead(err error) error {
	if errors.Is(err, domain.ErrBackupInvalid) {
		return err
	}
	return invalid("damaged stream: %v", err)
}

func nextEntry(tr *tar.Reader) (*tar.Header, error) {
	hd, err := tr.Next()
	if errors.Is(err, io.EOF) {
		return nil, invalid("truncated: %s missing", digestName)
	}
	if err != nil {
		return nil, invalidRead(err)
	}
	if hd.Typeflag != tar.TypeReg {
		return nil, invalid("entry %q is not a regular file", hd.Name)
	}
	return hd, nil
}

// Decode implements ports.BackupCodec.
func (c *Codec) Decode(ctx context.Context, r io.Reader, sink ports.BackupSink) (domain.BackupHeader, domain.BackupDigest, error) {
	// Our own buffered reader (an io.ByteReader, so gzip reads through it without buffering
	// further) lets us check that nothing follows the gzip member.
	br := bufio.NewReader(r)
	gz, err := gzip.NewReader(br)
	if err != nil {
		return domain.BackupHeader{}, domain.BackupDigest{}, invalidRead(err)
	}
	gz.Multistream(false)
	limited := &countingReader{r: gz, max: c.limits.MaxTotal}
	tr := tar.NewReader(limited)

	hd, err := nextEntry(tr)
	if err != nil {
		return domain.BackupHeader{}, domain.BackupDigest{}, err
	}
	if hd.Name != headerName {
		return domain.BackupHeader{}, domain.BackupDigest{}, invalid("first entry is %q, not %s", hd.Name, headerName)
	}
	data, err := readSmall(tr, hd, c.limits.MaxHeader)
	if err != nil {
		return domain.BackupHeader{}, domain.BackupDigest{}, err
	}
	h, err := domain.ParseBackupHeader(data)
	if err != nil {
		return domain.BackupHeader{}, domain.BackupDigest{}, err
	}
	if err := sink.Header(ctx, h); err != nil {
		return h, domain.BackupDigest{}, err
	}

	computed, declared, err := c.decodeBody(ctx, tr, h, sink)
	if err != nil {
		return h, domain.BackupDigest{}, err
	}
	// The stream must end here: tar's end marker, then the end of the gzip member (which
	// also checks its CRC).
	if _, err := tr.Next(); !errors.Is(err, io.EOF) {
		return h, domain.BackupDigest{}, invalid("data after %s", digestName)
	}
	if _, err := io.Copy(io.Discard, limited); err != nil {
		return h, domain.BackupDigest{}, invalidRead(err)
	}
	// Read the input to its end (an encrypting reader authenticates its last chunk there):
	// nothing may follow the stream.
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		if err != nil {
			return h, domain.BackupDigest{}, invalidRead(err)
		}
		return h, domain.BackupDigest{}, invalid("data after the stream")
	}
	if err := domain.VerifyBackupDigest(declared, computed); err != nil {
		return h, domain.BackupDigest{}, err
	}
	return h, declared, nil
}

// decodeBody reads the table parts in header order and the digest; it returns the digest
// computed from the rows and the one declared.
func (c *Codec) decodeBody(ctx context.Context, tr *tar.Reader, h domain.BackupHeader, sink ports.BackupSink) (domain.BackupDigest, domain.BackupDigest, error) {
	var computed domain.BackupDigest
	ti, wantPart := 0, 1
	digests := make([]*tableDigest, len(h.Tables))
	for i := range digests {
		digests[i] = newTableDigest()
	}
	for {
		if err := ctx.Err(); err != nil {
			return computed, domain.BackupDigest{}, err
		}
		hd, err := nextEntry(tr)
		if err != nil {
			return computed, domain.BackupDigest{}, err
		}
		if hd.Name == digestName {
			data, err := readSmall(tr, hd, c.limits.MaxHeader)
			if err != nil {
				return computed, domain.BackupDigest{}, err
			}
			declared, err := domain.ParseBackupDigest(data)
			if err != nil {
				return computed, domain.BackupDigest{}, err
			}
			for i, t := range h.Tables {
				computed.Tables = append(computed.Tables, digests[i].result(t.Name))
			}
			return computed, declared, nil
		}
		m := partRe.FindStringSubmatch(hd.Name)
		if m == nil {
			return computed, domain.BackupDigest{}, invalid("unexpected entry %q", hd.Name)
		}
		// Parts come in header table order, numbered from 1 without gaps.
		for ti < len(h.Tables) && h.Tables[ti].Name != m[1] {
			ti, wantPart = ti+1, 1
		}
		if ti == len(h.Tables) || m[2] != fmt.Sprintf("%06d", wantPart) {
			return computed, domain.BackupDigest{}, invalid("entry %q is out of order", hd.Name)
		}
		wantPart++
		if hd.Size > int64(c.limits.PartSize)+int64(c.limits.MaxRow)+1 {
			return computed, domain.BackupDigest{}, invalid("%s is too large", hd.Name)
		}
		if err := c.decodePart(ctx, io.LimitReader(tr, hd.Size), h.Tables[ti], digests[ti], &computed.AuditHead, sink); err != nil {
			return computed, domain.BackupDigest{}, err
		}
	}
}

func (c *Codec) decodePart(ctx context.Context, r io.Reader, t domain.BackupTable, td *tableDigest, head *domain.AuditRef, sink ports.BackupSink) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, c.limits.MaxRow)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		row, err := decodeRow(t, line[:len(line)-1])
		if err != nil {
			return err
		}
		td.add(line)
		if ref, ok := domain.AuditHeadOf(t, row); ok {
			*head = ref
		}
		if err := sink.Row(ctx, t.Name, row); err != nil {
			return err
		}
	}
}

// readLine returns the next line including its newline; io.EOF at a clean end.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		frag, err := br.ReadSlice('\n')
		line = append(line, frag...)
		if len(line) > max+1 {
			return nil, invalid("a row is larger than %d bytes", max)
		}
		switch {
		case err == nil:
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(line) == 0:
			return nil, io.EOF
		case errors.Is(err, io.EOF):
			return nil, invalid("a row does not end with a newline")
		default:
			return nil, invalidRead(err)
		}
	}
}
