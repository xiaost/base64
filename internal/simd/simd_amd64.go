//go:build !purego

package simd

const (
	stdAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	urlAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

var hasAVX2, hasSSSE3 = detectCPU()

// Active returns the name of the kernel set in use, for diagnostics.
func Active() string {
	switch {
	case hasAVX2:
		return "avx2"
	case hasSSSE3:
		return "ssse3"
	}
	return ""
}

// Minimum src lengths below which Encode / Decode are no-ops.
var MinEncode, MinDecode = minSizes()

func minSizes() (enc, dec int) {
	if hasAVX2 {
		return 24 + 4, 32
	}
	return 12 + 4, 16
}

func detectCPU() (avx2, ssse3 bool) {
	maxID, _, _, _ := cpuidex(0, 0)
	_, _, c, _ := cpuidex(1, 0)
	ssse3 = c&(1<<9) != 0
	const osxsaveAVX = 1<<27 | 1<<28
	if maxID < 7 || c&osxsaveAVX != osxsaveAVX {
		return false, ssse3
	}
	if x, _ := xgetbv0(); x&6 != 6 { // XMM and YMM state enabled by OS
		return false, ssse3
	}
	_, b, _, _ := cpuidex(7, 0)
	return b&(1<<5) != 0, ssse3 // AVX2
}

// Encoder holds the AVX2 translation table (Muła's range-shift scheme).
// It supports any alphabet whose first 62 chars match the standard one;
// chars 62 and 63 are free.
type Encoder struct {
	lut [16]byte
}

func NewEncoder(alphabet *[64]byte) *Encoder {
	if (!hasAVX2 && !hasSSSE3) || string(alphabet[:62]) != stdAlphabet[:62] {
		return nil
	}
	e := &Encoder{}
	e.lut[0] = alphabet[26] - 26 // 'a'-26, for indices 26..51
	for i := 1; i <= 10; i++ {
		e.lut[i] = alphabet[52] - 52 // '0'-52 (wraps), for indices 52..61
	}
	e.lut[11] = alphabet[62] - 62
	e.lut[12] = alphabet[63] - 63
	e.lut[13] = alphabet[0] // +'A', for indices 0..25
	return e
}

// Decoder holds the AVX2 classification tables (Muła's nibble scheme):
// five 32-byte vectors at fixed offsets: lo-nibble mask, hi-nibble mask,
// roll lut, marker char, marker index adjustment.
type Decoder struct {
	tab [160]byte
}

func NewDecoder(alphabet *[64]byte) *Decoder {
	if !hasAVX2 && !hasSSSE3 {
		return nil
	}
	var lo, hi, roll [16]byte
	var marker, adj byte
	switch string(alphabet[:]) {
	case stdAlphabet:
		lo = [16]byte{0x15, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x13, 0x1A, 0x1B, 0x1B, 0x1B, 0x1A}
		hi = [16]byte{0x10, 0x10, 0x01, 0x02, 0x04, 0x08, 0x04, 0x08, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10}
		roll = [16]byte{0, 16, 19, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker, adj = '/', 0xFF // '/': roll idx = hi(2)-1 = 1
	case urlAlphabet:
		lo = [16]byte{0x25, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x23, 0x3B, 0x3B, 0x3A, 0x3B, 0x33}
		hi = [16]byte{0x20, 0x20, 0x01, 0x02, 0x04, 0x08, 0x04, 0x10, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		roll = [16]byte{0, 0, 17, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0xE0, 0, 0}
		marker, adj = '_', 8 // '_': roll idx = hi(5)+8 = 13
	default:
		return nil
	}
	d := &Decoder{}
	for i := range 32 {
		d.tab[i] = lo[i%16]
		d.tab[32+i] = hi[i%16]
		d.tab[64+i] = roll[i%16]
		d.tab[96+i] = marker
		d.tab[128+i] = adj
	}
	return d
}

// Encode converts whole blocks of src (24 bytes with AVX2, 12 with SSSE3)
// into blocks of dst (32 / 16 bytes). It returns the number of bytes
// written and consumed. The kernels load 4 bytes past each block, so 4
// readable bytes are kept past the consumed range.
func (e *Encoder) Encode(dst, src []byte) (nd, ns int) {
	bin, bout := 12, 16
	if hasAVX2 {
		bin, bout = 24, 32
	}
	if len(src) < bin+4 {
		return 0, 0
	}
	n := (len(src) - 4) / bin
	if m := len(dst) / bout; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	if hasAVX2 {
		encodeAVX2(&dst[0], &src[0], n, &e.lut[0])
	} else {
		encodeSSE(&dst[0], &src[0], n, &e.lut[0])
	}
	return n * bout, n * bin
}

// Decode converts whole blocks of src (32 bytes with AVX2, 16 with SSSE3)
// into blocks of dst (24 / 12 bytes), stopping at the first block
// containing a byte that is not part of the alphabet (padding, newlines,
// garbage). It returns the number of bytes written and consumed.
func (d *Decoder) Decode(dst, src []byte) (nd, ns int) {
	bin, bout := 16, 12
	if hasAVX2 {
		bin, bout = 32, 24
	}
	n := len(src) / bin
	if m := len(dst) / bout; m < n {
		n = m
	}
	if n == 0 {
		return 0, 0
	}
	if hasAVX2 {
		ns = decodeAVX2(&dst[0], &src[0], n, &d.tab[0])
	} else {
		ns = decodeSSE(&dst[0], &src[0], n, &d.tab[0])
	}
	return ns / 4 * 3, ns
}

//go:noescape
func encodeAVX2(dst, src *byte, blocks int, lut *byte)

//go:noescape
func decodeAVX2(dst, src *byte, blocks int, tab *byte) int

//go:noescape
func encodeSSE(dst, src *byte, blocks int, lut *byte)

//go:noescape
func decodeSSE(dst, src *byte, blocks int, tab *byte) int

func cpuidex(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv0() (eax, edx uint32)
