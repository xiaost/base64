package base64

import (
	"bytes"
	"slices"
	"unsafe"

	"github.com/xiaost/base64/internal/simd"
)

// Decode decodes src using the encoding enc. It writes at most
// DecodedLen(len(src)) bytes to dst and returns the number of bytes
// written. The caller must ensure that dst is large enough to hold all
// the decoded data. If src contains invalid base64 data, it will return
// the number of bytes successfully written and CorruptInputError.
// Newline characters (\r and \n) are ignored.
func (e *Encoding) Decode(dst, src []byte) (n int, err error) {
	if len(src) == 0 {
		return 0, nil
	}
	var nd, ns int
	if e.dec != nil && len(src) >= simd.MinDecode {
		nd, ns = e.dec.Decode(dst, src)
		if len(src)-ns >= simd.MinDecode {
			// Substantial remainder: maybe stopped by a newline.
			nd, ns = e.decodeSIMD(dst, src, nd, ns)
		}
	}
	n, err = e.std.Decode(dst[nd:], src[ns:])
	n += nd
	if err != nil {
		if pos, ok := err.(CorruptInputError); ok {
			err = CorruptInputError(int64(pos) + int64(ns))
		}
	}
	return n, err
}

// nlWin bounds the search for a newline after a kernel stop: the byte
// that stopped the kernel lies within one block (at most 64 chars), so
// a newline past that distance was not the reason it stopped.
const nlWin = 68

// decodeSIMD picks up after the caller's kernel call stopped with a
// substantial remainder, re-entering the kernel across runs of newlines
// at 4-char quantum boundaries (line-wrapped base64: PEM, MIME). Plain
// alphabet quanta between a kernel stop and the newline are decoded
// scalar. It only ever consumes whole quanta of plain alphabet chars
// and the newlines between them, so the stdlib decodes the remainder
// src[ns:] exactly as it would have in place.
func (e *Encoding) decodeSIMD(dst, src []byte, nd, ns int) (int, int) {
	stalls := 0
	s := ns // progress of the kernel call just made by the caller
	for {
		// The kernel stopped: dst full or a block holding a byte it
		// can't decode. Anything but a newline at a quantum boundary
		// is left to the stdlib.
		w := src[ns:min(ns+nlWin, len(src))]
		q := bytes.IndexByte(w, '\n')
		if r := bytes.IndexByte(w, '\r'); r >= 0 && (q < 0 || r < q) {
			q = r
		}
		if q < 0 || q%4 != 0 {
			return nd, ns
		}
		// Lines shorter than a kernel block make no SIMD progress;
		// after two stalled rounds hand the rest to the stdlib.
		if s > 0 {
			stalls = 0
		} else if stalls++; stalls == 2 {
			return nd, ns
		}
		// Bridge the quanta between the stop and the newline, but only
		// whole quanta of plain alphabet chars: a padded quantum decodes
		// differently when followed by more data ("trailing garbage"),
		// so it must stay in the stdlib tail with everything after it.
		for i := 0; i+4 <= q; i += 4 {
			v0, v1, v2, v3 := e.dmap[w[i]], e.dmap[w[i+1]], e.dmap[w[i+2]], e.dmap[w[i+3]]
			if v0|v1|v2|v3 >= 0x40 {
				return nd, ns
			}
			// Highest index first: a too-short dst panics before any
			// partial write, like the stdlib's decodeQuantum.
			dst[nd+2] = v2<<6 | v3
			dst[nd+1] = v1<<4 | v2>>2
			dst[nd] = v0<<2 | v1>>4
			nd += 3
			ns += 4
		}
		for ns < len(src) && (src[ns] == '\n' || src[ns] == '\r') {
			ns++
		}
		if len(src)-ns < simd.MinDecode {
			return nd, ns
		}
		var d int
		d, s = e.dec.Decode(dst[nd:], src[ns:])
		nd += d
		ns += s
		if len(src)-ns < simd.MinDecode {
			return nd, ns
		}
	}
}

// AppendDecode appends the base64 decoded src to dst and returns the
// extended buffer. If the input is malformed, it returns the partially
// decoded src and an error.
func (e *Encoding) AppendDecode(dst, src []byte) ([]byte, error) {
	// Compute the output size without padding to avoid over allocating.
	n := len(src)
	for n > 0 && rune(src[n-1]) == e.pad {
		n--
	}
	n = n/4*3 + n%4*6/8 // decoded length without padding, like the stdlib

	dst = slices.Grow(dst, n)
	n, err := e.Decode(dst[len(dst):][:n], src)
	return dst[:len(dst)+n], err
}

// DecodeString returns the bytes represented by the base64 string s.
func (e *Encoding) DecodeString(s string) ([]byte, error) {
	buf := make([]byte, e.DecodedLen(len(s)))
	if len(s) == 0 {
		return buf[:0], nil
	}
	src := unsafe.Slice(unsafe.StringData(s), len(s)) // read-only
	n, err := e.Decode(buf, src)
	return buf[:n], err
}
