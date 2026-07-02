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
// The SSE kernels handle single 12-byte / 16-char blocks left over by
// the AVX2 loop, so the minimums match the SSE block sizes either way.
const (
	MinEncode = 12
	MinDecode = 16
)

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
// four 32-byte vectors at fixed offsets: lo-nibble mask, hi-nibble mask,
// roll lut, marker char.
//
// The kernels turn the marker char's roll index into 0 (hi &^ eq-mask),
// so roll[0] holds the marker's shift. Hi nibbles 0 and 1 also land on
// roll[0], but such bytes never pass the invalid-char masks.
type Decoder struct {
	tab [128]byte
}

func NewDecoder(alphabet *[64]byte) *Decoder {
	if !hasAVX2 && !hasSSSE3 {
		return nil
	}
	var lo, hi, roll [16]byte
	var marker byte
	switch string(alphabet[:]) {
	case stdAlphabet:
		lo = [16]byte{0x15, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x13, 0x1A, 0x1B, 0x1B, 0x1B, 0x1A}
		hi = [16]byte{0x10, 0x10, 0x01, 0x02, 0x04, 0x08, 0x04, 0x08, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10}
		roll = [16]byte{16, 0, 19, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker = '/'
	case urlAlphabet:
		lo = [16]byte{0x25, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x21, 0x23, 0x3B, 0x3B, 0x3A, 0x3B, 0x33}
		hi = [16]byte{0x20, 0x20, 0x01, 0x02, 0x04, 0x08, 0x04, 0x10, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		roll = [16]byte{0xE0, 0, 17, 4, 0xBF, 0xBF, 0xB9, 0xB9, 0, 0, 0, 0, 0, 0, 0, 0}
		marker = '_'
	default:
		return nil
	}
	d := &Decoder{}
	for i := range 32 {
		d.tab[i] = lo[i%16]
		d.tab[32+i] = hi[i%16]
		d.tab[64+i] = roll[i%16]
		d.tab[96+i] = marker
	}
	return d
}

// Encode converts whole blocks of src (24 bytes with AVX2, 12 with SSSE3)
// into blocks of dst (32 / 16 bytes), plus one SSE block for a 12-byte
// remainder the AVX2 loop leaves behind. It returns the number of bytes
// written and consumed.
func (e *Encoder) Encode(dst, src []byte) (nd, ns int) {
	if hasAVX2 {
		n := len(src) / 24
		if m := len(dst) / 32; m < n {
			n = m
		}
		if n > 0 {
			consumed := n * 24
			// The loop kernels read 4 bytes past their final block; switch
			// only the final block to the bounded kernel when src ends there.
			if len(src) >= consumed+4 {
				encodeAVX2(&dst[0], &src[0], n, &e.lut[0])
			} else {
				full := n - 1
				if full > 0 {
					encodeAVX2(&dst[0], &src[0], full, &e.lut[0])
				}
				s, d := full*24, full*32
				encodeAVX2Last(&dst[d], &src[s], &e.lut[0])
			}
			nd, ns = n*32, consumed
		}
		// An AVX2 pass can leave one 12-byte block, which the SSE kernel can
		// still encode before the caller falls back for the shorter tail.
		if len(src)-ns >= 12 && len(dst)-nd >= 16 {
			if len(src)-ns >= 16 {
				encodeSSE(&dst[nd], &src[ns], 1, &e.lut[0])
			} else {
				encodeSSELast(&dst[nd], &src[ns], &e.lut[0])
			}
			nd += 16
			ns += 12
		}
		return nd, ns
	}

	n := len(src) / 12
	if m := len(dst) / 16; m < n {
		n = m
	}
	if n > 0 {
		consumed := n * 12
		// SSSE3 has the same 4-byte over-read constraint as AVX2, but its
		// final-block kernel handles a single 12-byte block directly.
		if len(src) >= consumed+4 {
			encodeSSE(&dst[0], &src[0], n, &e.lut[0])
			nd, ns = n*16, consumed
		} else {
			full := n - 1
			if full > 0 {
				encodeSSE(&dst[0], &src[0], full, &e.lut[0])
			}
			s, d := full*12, full*16
			encodeSSELast(&dst[d], &src[s], &e.lut[0])
			nd, ns = n*16, consumed
		}
	}
	return nd, ns
}

// Decode converts whole blocks of src (32 bytes with AVX2, 16 with SSSE3)
// into blocks of dst (24 / 12 bytes), plus one SSE block for a 16-char
// remainder the AVX2 loop leaves behind. It stops at the first block
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
	if n > 0 {
		if hasAVX2 {
			ns = decodeAVX2(&dst[0], &src[0], n, &d.tab[0])
		} else {
			ns = decodeSSE(&dst[0], &src[0], n, &d.tab[0])
		}
		nd = ns / 4 * 3
		if ns < n*bin {
			return nd, ns // stopped at an invalid byte
		}
	}
	if hasAVX2 && len(src)-ns >= 16 && len(dst)-nd >= 12 {
		s := decodeSSE(&dst[nd], &src[ns], 1, &d.tab[0])
		nd += s / 4 * 3
		ns += s
	}
	return nd, ns
}

//go:noescape
func encodeAVX2(dst, src *byte, blocks int, lut *byte)

//go:noescape
func encodeAVX2Last(dst, src *byte, lut *byte)

//go:noescape
func decodeAVX2(dst, src *byte, blocks int, tab *byte) int

//go:noescape
func encodeSSE(dst, src *byte, blocks int, lut *byte)

//go:noescape
func encodeSSELast(dst, src *byte, lut *byte)

//go:noescape
func decodeSSE(dst, src *byte, blocks int, tab *byte) int

func cpuidex(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)

func xgetbv0() (eax, edx uint32)
