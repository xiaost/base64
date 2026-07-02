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
	MinEncode = 24
	MinDecode = 16
)

// Encoder holds the NEON encode table: the 64-byte alphabet itself, looked
// up with TBL. Any alphabet is supported.
type Encoder struct {
	lut [64]byte
}

// Decoder holds the NEON decode tables. Standard and URL alphabets use
// the 64-byte nibble table; custom alphabets use the 256-byte LUT.
type Decoder struct {
	lut [256]byte
	tab *[64]byte
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

// The kernels turn the marker char's roll index into 0 (hi &^ eq-mask),
// so roll[0] holds the marker's shift. Hi nibbles 0 and 1 also land on
// roll[0], but such bytes never pass the invalid-char masks.
func decodeTab(url bool) [64]byte {
	var lo, hi, roll [16]byte
	var marker byte
	if url {
		lo = [16]byte{0x25, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x23, 0x3B, 0x3B, 0x3A, 0x3B, 0x33}
		hi = [16]byte{0x20, 0x20, 0x01, 0x02, 0x04, 0x08, 0x04, 0x10, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		roll = [16]byte{0xE0, 0, 17, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker = '_'
	} else {
		lo = [16]byte{0x15, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x13, 0x1A, 0x1B, 0x1B, 0x1B, 0x1A}
		hi = [16]byte{0x10, 0x10, 0x01, 0x02, 0x04, 0x08, 0x04, 0x08, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10}
		roll = [16]byte{16, 0, 19, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker = '/'
	}
	var t [64]byte
	copy(t[0:], lo[:])
	copy(t[16:], hi[:])
	copy(t[32:], roll[:])
	for i := range 16 {
		t[48+i] = marker
	}
	return t
}

// Encode converts whole 24-byte blocks of src into 32-byte blocks of dst.
// It returns the number of bytes written and consumed.
func (e *Encoder) Encode(dst, src []byte) (nd, ns int) {
	n := len(src) / 24
	if m := len(dst) / 32; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	encodeNEON(&dst[0], &src[0], n*24, &e.lut[0])
	return n * 32, n * 24
}

// Decode converts whole 16-char blocks of src (64 for custom alphabets)
// into 12-byte blocks of dst, stopping at the first block containing a
// byte that is not part of the alphabet (padding, newlines, garbage).
// It returns the number of bytes written and consumed.
func (d *Decoder) Decode(dst, src []byte) (nd, ns int) {
	bin, bout := 16, 12
	if d.tab == nil {
		bin, bout = 64, 48
	}
	n := len(src) / bin
	if m := len(dst) / bout; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	if d.tab != nil {
		ns = decodeNEONFast(&dst[0], &src[0], n*16, &d.tab[0])
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
