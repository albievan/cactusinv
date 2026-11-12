package pdf

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5" //nolint:gosec // the PDF standard security handler (ISO 32000-1 §7.6.3) is built on MD5 and RC4
	"crypto/rand"
	"crypto/rc4" //nolint:gosec // as above: it derives the O and U entries, not the content key
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// The quote PDF's protection (M34) is the PDF standard security handler, revision 4
// (ISO 32000-1 §7.6.3): every string and stream is encrypted with AES-128 under a key of
// its own, there is no password to open the document, and the permissions grant printing
// only. The owner password, which a reader would need to lift the permissions, is random
// and never kept. fpdf's own SetProtection is not used: its RC4 carries the keystream from
// one string of an object on to the next, so every string but the first (the document
// title, a second link on a page) came out unreadable.

// printOnlyP is the permission flags (ISO 32000-1 table 22): print (bit 3) at full quality
// (bit 12); no changes (4), copying (5), annotations (6), form filling (9), extraction (10)
// or assembly (11). Bits 7–8 and 13–32 are 1, as revision 4 requires.
const printOnlyP = int32(-1852) // 0xFFFFF8C4

// printOnlyBytes is printOnlyP as the key derivation reads it: 4 bytes, low-order first.
var printOnlyBytes = []byte{0xC4, 0xF8, 0xFF, 0xFF}

// passwordPad is the standard padding of a password (ISO 32000-1 §7.6.3.3, step a).
var passwordPad = []byte{
	0x28, 0xBF, 0x4E, 0x5E, 0x4E, 0x75, 0x8A, 0x41, 0x64, 0x00, 0x4E, 0x56, 0xFF, 0xFA, 0x01, 0x08,
	0x2E, 0x2E, 0x00, 0xB6, 0xD0, 0x68, 0x3E, 0x80, 0x2F, 0x0C, 0xA9, 0xFE, 0x64, 0x53, 0x69, 0x7A,
}

func padPassword(pw []byte) []byte {
	return append(append([]byte{}, pw[:min(len(pw), 32)]...), passwordPad...)[:32]
}

// rc4Rounds encrypts data with key, then 19 more times with key XOR 1…19 (revision 3+).
func rc4Rounds(key, data []byte) []byte {
	out := append([]byte{}, data...)
	k := make([]byte, len(key))
	for i := range 20 {
		for j := range key {
			k[j] = key[j] ^ byte(i)
		}
		c, _ := rc4.NewCipher(k) //nolint:gosec // see the import
		c.XORKeyStream(out, out)
	}
	return out
}

// md5Rounds is MD5 of data, then 50 more times of the first 16 bytes (revision 3+).
func md5Rounds(data []byte) []byte {
	h := md5.Sum(data) //nolint:gosec // see the import
	for range 50 {
		h = md5.Sum(h[:16]) //nolint:gosec // see the import
	}
	return h[:16]
}

// securityHandler holds a document's keys.
type securityHandler struct {
	fileKey []byte
	o, u    []byte
	id      []byte
}

// newSecurityHandler makes the keys for a document with file identifier id, no user
// password and the owner password owner (algorithms 2, 3 and 5).
func newSecurityHandler(owner, id []byte) *securityHandler {
	user := padPassword(nil)
	o := rc4Rounds(md5Rounds(padPassword(owner)), user)
	key := md5Rounds(bytes.Join([][]byte{user, o, printOnlyBytes, id}, nil))
	h := md5.Sum(append(append([]byte{}, passwordPad...), id...)) //nolint:gosec // see the import
	u := append(rc4Rounds(key, h[:]), make([]byte, 16)...)
	return &securityHandler{fileKey: key, o: o, u: u, id: id}
}

// objectKey is the AES key of object num (algorithm 1, with the "sAlT" of AESV2).
func (s *securityHandler) objectKey(num int) []byte {
	b := append([]byte{}, s.fileKey...)
	b = append(b, byte(num), byte(num>>8), byte(num>>16), 0, 0) //nolint:gosec // the low 3 bytes of the object number, as the standard says
	b = append(b, "sAlT"...)
	h := md5.Sum(b) //nolint:gosec // see the import
	return h[:16]
}

// seal encrypts data for object num: a random IV, then AES-128-CBC with PKCS#5 padding.
func (s *securityHandler) seal(num int, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(s.objectKey(num))
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	plain := append(append([]byte{}, data...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	out := make([]byte, aes.BlockSize+len(plain))
	if _, err := rand.Read(out[:aes.BlockSize]); err != nil {
		return nil, err
	}
	cipher.NewCBCEncrypter(block, out[:aes.BlockSize]).CryptBlocks(out[aes.BlockSize:], plain) //nolint:gosec // the IV is random (crypto/rand above)
	return out, nil
}

var (
	objHeader  = regexp.MustCompile(`^(\d+) (\d+) obj\s*`)
	lengthKey  = regexp.MustCompile(`/Length (\d+)([\s/>])`)
	trailerRef = regexp.MustCompile(`/(Root|Info) (\d+ \d+ R)`)
	trailerSz  = regexp.MustCompile(`/Size (\d+)`)
)

var errPDFShape = errors.New("pdf: unexpected document structure for encryption")

// encryptPrintOnly rewrites an unencrypted PDF (as fpdf writes it: numbered objects, a
// cross-reference table and a trailer) encrypted print-only, with a random owner password.
func encryptPrintOnly(in []byte) ([]byte, error) {
	owner, id := make([]byte, 32), make([]byte, 16)
	if _, err := rand.Read(owner); err != nil {
		return nil, err
	}
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	sh := newSecurityHandler(owner, id)

	if !bytes.HasPrefix(in, []byte("%PDF-")) {
		return nil, errPDFShape
	}
	pos := bytes.IndexByte(in, '\n') + 1
	var out bytes.Buffer
	out.WriteString("%PDF-1.6\n%\xe2\xe3\xcf\xd3\n") // AESV2 needs PDF 1.6
	offsets := map[int]int{}
	for {
		pos = skipSpace(in, pos)
		if bytes.HasPrefix(in[pos:], []byte("xref")) {
			break
		}
		m := objHeader.FindSubmatch(in[pos:])
		if m == nil {
			return nil, fmt.Errorf("%w: no object at %d", errPDFShape, pos)
		}
		num, _ := strconv.Atoi(string(m[1]))
		pos += len(m[0])
		offsets[num] = out.Len()
		fmt.Fprintf(&out, "%d %s obj\n", num, m[2])
		next, err := sh.object(&out, in, pos, num)
		if err != nil {
			return nil, err
		}
		pos = next
	}
	tail := in[pos:]
	t := bytes.Index(tail, []byte("trailer"))
	if t < 0 {
		return nil, fmt.Errorf("%w: no trailer", errPDFShape)
	}
	trailer := tail[t:]
	sm := trailerSz.FindSubmatch(trailer)
	if sm == nil {
		return nil, fmt.Errorf("%w: no /Size", errPDFShape)
	}
	size, _ := strconv.Atoi(string(sm[1]))
	refs := map[string]string{}
	for _, r := range trailerRef.FindAllSubmatch(trailer, -1) {
		refs[string(r[1])] = string(r[2])
	}
	if refs["Root"] == "" {
		return nil, fmt.Errorf("%w: no /Root", errPDFShape)
	}
	encNum := size
	offsets[encNum] = out.Len()
	fmt.Fprintf(&out, "%d 0 obj\n<<\n/Filter /Standard\n/V 4\n/R 4\n/Length 128\n/CF << /StdCF << /CFM /AESV2 /AuthEvent /DocOpen /Length 16 >> >>\n/StmF /StdCF\n/StrF /StdCF\n/O <%x>\n/U <%x>\n/P %d\n>>\nendobj\n",
		encNum, sh.o, sh.u, printOnlyP)

	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", size+1)
	for n := 1; n <= size; n++ {
		if off, ok := offsets[n]; ok {
			fmt.Fprintf(&out, "%010d 00000 n \n", off)
		} else {
			out.WriteString("0000000000 65535 f \n")
		}
	}
	idHex := hex.EncodeToString(id)
	fmt.Fprintf(&out, "trailer\n<<\n/Size %d\n/Root %s\n", size+1, refs["Root"])
	if refs["Info"] != "" {
		fmt.Fprintf(&out, "/Info %s\n", refs["Info"])
	}
	fmt.Fprintf(&out, "/Encrypt %d 0 R\n/ID [<%s> <%s>]\n>>\nstartxref\n%d\n%%%%EOF\n", encNum, idHex, idHex, xref)
	return out.Bytes(), nil
}

func skipSpace(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\n' || b[i] == '\r' || b[i] == '\t') {
		i++
	}
	return i
}

// object copies the body of object num from in[pos:] to out with its strings and stream
// encrypted, and returns the position after its "endobj".
func (s *securityHandler) object(out *bytes.Buffer, in []byte, pos, num int) (int, error) {
	var body bytes.Buffer
	i := pos
	for i < len(in) {
		c := in[i]
		switch {
		case c == '(':
			str, next, err := literalString(in, i)
			if err != nil {
				return 0, err
			}
			if err := s.writeString(&body, num, str); err != nil {
				return 0, err
			}
			i = next
		case c == '<' && i+1 < len(in) && in[i+1] == '<':
			body.WriteString("<<")
			i += 2
		case c == '<':
			end := bytes.IndexByte(in[i:], '>')
			if end < 0 {
				return 0, fmt.Errorf("%w: unterminated hex string", errPDFShape)
			}
			str, err := hexString(in[i+1 : i+end])
			if err != nil {
				return 0, err
			}
			if err := s.writeString(&body, num, str); err != nil {
				return 0, err
			}
			i += end + 1
		case keywordAt(in, i, "endobj"):
			out.Write(body.Bytes())
			out.WriteString("endobj\n")
			return i + len("endobj"), nil
		case keywordAt(in, i, "stream"):
			dict := body.Bytes()
			m := lengthKey.FindSubmatchIndex(dict)
			if m == nil {
				return 0, fmt.Errorf("%w: a stream without a direct /Length", errPDFShape)
			}
			n, _ := strconv.Atoi(string(dict[m[2]:m[3]]))
			start := i + len("stream")
			if start < len(in) && in[start] == '\r' {
				start++
			}
			if start >= len(in) || in[start] != '\n' || start+1+n > len(in) {
				return 0, fmt.Errorf("%w: a stream runs past the end", errPDFShape)
			}
			start++
			sealed, err := s.seal(num, in[start:start+n])
			if err != nil {
				return 0, err
			}
			out.Write(dict[:m[2]])
			out.WriteString(strconv.Itoa(len(sealed)))
			out.Write(dict[m[3]:])
			out.WriteString("stream\n")
			out.Write(sealed)
			out.WriteString("\n")
			i = skipSpace(in, start+n)
			if !keywordAt(in, i, "endstream") {
				return 0, fmt.Errorf("%w: a stream is not ended where its /Length says", errPDFShape)
			}
			body.Reset()
			body.WriteString("endstream\n")
			i += len("endstream")
		default:
			body.WriteByte(c)
			i++
		}
	}
	return 0, fmt.Errorf("%w: object %d is not ended", errPDFShape, num)
}

func (s *securityHandler) writeString(out *bytes.Buffer, num int, str []byte) error {
	sealed, err := s.seal(num, str)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "<%x>", sealed)
	return nil
}

// keywordAt reports whether keyword kw starts at in[i] and stands alone (not part of a
// longer name such as "endstream").
func keywordAt(in []byte, i int, kw string) bool {
	if !bytes.HasPrefix(in[i:], []byte(kw)) {
		return false
	}
	before := i == 0 || !isRegular(in[i-1])
	after := i+len(kw) >= len(in) || !isRegular(in[i+len(kw)])
	return before && after
}

// isRegular reports whether c is a PDF regular character (not white space or a delimiter).
func isRegular(c byte) bool {
	switch c {
	case ' ', '\n', '\r', '\t', '\f', 0, '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return false
	}
	return true
}

// literalString reads the literal string starting at in[i] == '(' and returns its bytes
// and the position after its closing parenthesis (ISO 32000-1 §7.3.4.2).
func literalString(in []byte, i int) ([]byte, int, error) {
	var out []byte
	depth := 0
	for j := i; j < len(in); j++ {
		c := in[j]
		switch c {
		case '\\':
			j++
			if j >= len(in) {
				return nil, 0, fmt.Errorf("%w: unterminated string", errPDFShape)
			}
			switch e := in[j]; e {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			case '\r':
				if j+1 < len(in) && in[j+1] == '\n' {
					j++
				}
			case '\n':
			default:
				if e >= '0' && e <= '7' {
					v := 0
					k := 0
					for ; k < 3 && j+k < len(in) && in[j+k] >= '0' && in[j+k] <= '7'; k++ {
						v = v*8 + int(in[j+k]-'0')
					}
					j += k - 1
					out = append(out, byte(v))
				} else {
					out = append(out, e) // \( \) \\ and any other character itself
				}
			}
		case '(':
			if depth > 0 {
				out = append(out, c)
			}
			depth++
		case ')':
			depth--
			if depth == 0 {
				return out, j + 1, nil
			}
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	return nil, 0, fmt.Errorf("%w: unterminated string", errPDFShape)
}

func hexString(b []byte) ([]byte, error) {
	clean := make([]byte, 0, len(b)+1)
	for _, c := range b {
		if c != ' ' && c != '\n' && c != '\r' && c != '\t' && c != '\f' {
			clean = append(clean, c)
		}
	}
	if len(clean)%2 == 1 {
		clean = append(clean, '0')
	}
	out := make([]byte, len(clean)/2)
	if _, err := hex.Decode(out, clean); err != nil {
		return nil, fmt.Errorf("%w: bad hex string", errPDFShape)
	}
	return out, nil
}
