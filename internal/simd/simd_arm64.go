//go:build !purego

package simd

const (
	stdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	urlAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

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

// Decoder holds the NEON decode tables. Standard and URL alphabets use
// the 80-byte nibble table; custom alphabets use the 256-byte LUT.
type Decoder struct {
	lut [256]byte
	tab *[80]byte
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
	switch string(alphabet[:]) {
	case stdAlphabet:
		d.tab = &stdDecodeTab
	case urlAlphabet:
		d.tab = &urlDecodeTab
	}
	return d
}

var (
	stdDecodeTab = decodeTab(false)
	urlDecodeTab = decodeTab(true)
)

func decodeTab(url bool) [80]byte {
	var lo, hi, roll [16]byte
	var marker, adj byte
	if url {
		lo = [16]byte{0x25, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x23, 0x3B, 0x3B, 0x3A, 0x3B, 0x33}
		hi = [16]byte{0x20, 0x20, 0x01, 0x02, 0x04, 0x08, 0x04, 0x10, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		roll = [16]byte{0, 0, 17, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0xE0, 0, 0}
		marker, adj = '_', 8
	} else {
		lo = [16]byte{0x15, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x13, 0x1A, 0x1B, 0x1B, 0x1B, 0x1A}
		hi = [16]byte{0x10, 0x10, 0x01, 0x02, 0x04, 0x08, 0x04, 0x08, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10}
		roll = [16]byte{0, 16, 19, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker, adj = '/', 0xFF
	}
	var t [80]byte
	copy(t[0:], lo[:])
	copy(t[16:], hi[:])
	copy(t[32:], roll[:])
	for i := range 16 {
		t[48+i] = marker
		t[64+i] = adj
	}
	return t
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
	if d.tab != nil {
		ns = decodeNEONFast(&dst[0], &src[0], n*64, &d.tab[0])
	} else {
		ns = decodeNEON(&dst[0], &src[0], n*64, &d.lut[0])
	}
	return ns / 4 * 3, ns
}

//go:noescape
func encodeNEON(dst, src *byte, n int, lut *byte)

//go:noescape
func decodeNEON(dst, src *byte, n int, lut *byte) int

//go:noescape
func decodeNEONFast(dst, src *byte, n int, tab *byte) int
