//go:build !purego

package simd

// Active returns the name of the kernel set in use, for diagnostics.
func Active() string { return "neon" }

// Minimum src lengths below which Encode / Decode are no-ops.
const (
	MinEncode = 48
	MinDecode = 64
)

// Encoder holds the NEON encode table: the 64-byte alphabet itself, looked
// up with TBL. Any alphabet is supported.
type Encoder struct {
	lut [64]byte
}

// Decoder holds the NEON decode table: a 256-byte char->value map with
// 0xFF marking invalid bytes, looked up as four 64-byte TBL chunks.
type Decoder struct {
	lut [256]byte
}

func NewEncoder(alphabet *[64]byte) *Encoder {
	return &Encoder{lut: *alphabet}
}

func NewDecoder(alphabet *[64]byte) *Decoder {
	d := &Decoder{}
	for i := range d.lut {
		d.lut[i] = 0xFF
	}
	// Last occurrence wins, matching encoding/base64's decodeMap.
	for i, c := range alphabet {
		d.lut[c] = byte(i)
	}
	return d
}

// Encode converts whole 48-byte blocks of src into 64-byte blocks of dst.
// It returns the number of bytes written and consumed.
func (e *Encoder) Encode(dst, src []byte) (nd, ns int) {
	n := len(src) / 48
	if m := len(dst) / 64; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	encodeNEON(&dst[0], &src[0], n*48, &e.lut[0])
	return n * 64, n * 48
}

// Decode converts whole 64-byte blocks of src into 48-byte blocks of dst,
// stopping at the first block containing a byte that is not part of the
// alphabet (padding, newlines, garbage). It returns the number of bytes
// written and consumed.
func (d *Decoder) Decode(dst, src []byte) (nd, ns int) {
	n := len(src) / 64
	if m := len(dst) / 48; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	ns = decodeNEON(&dst[0], &src[0], n*64, &d.lut[0])
	return ns / 4 * 3, ns
}

//go:noescape
func encodeNEON(dst, src *byte, n int, lut *byte)

//go:noescape
func decodeNEON(dst, src *byte, n int, lut *byte) int
