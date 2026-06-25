package base64

import (
	"slices"
	"unsafe"

	"github.com/xiaost/base64/internal/simd"
)

// Encode encodes src using the encoding enc, writing EncodedLen(len(src))
// bytes to dst. The encoding pads the output to a multiple of 4 bytes, so
// Encode is not appropriate for use on individual blocks of a large data
// stream; use NewEncoder instead.
func (e *Encoding) Encode(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	if e.enc != nil && len(src) >= simd.MinEncode {
		nd, ns := e.enc.Encode(dst, src)
		dst, src = dst[nd:], src[ns:]
		if len(src) == 0 {
			return
		}
	}
	e.std.Encode(dst, src)
}

// AppendEncode appends the base64 encoded src to dst and returns the
// extended buffer.
func (e *Encoding) AppendEncode(dst, src []byte) []byte {
	n := e.EncodedLen(len(src))
	dst = slices.Grow(dst, n)
	e.Encode(dst[len(dst):][:n], src)
	return dst[:len(dst)+n]
}

// EncodeToString returns the base64 encoding of src.
func (e *Encoding) EncodeToString(src []byte) string {
	buf := make([]byte, e.EncodedLen(len(src)))
	if len(buf) == 0 {
		return ""
	}
	e.Encode(buf, src)
	return unsafe.String(&buf[0], len(buf))
}
