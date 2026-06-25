package base64

import (
	"encoding/base64"
	"io"
)

// NewEncoder returns a new base64 stream encoder. Data written to the
// returned writer will be encoded using enc and then written to w.
// Base64 encodings operate in 4-byte blocks; when finished writing, the
// caller must Close the returned encoder to flush any partially written
// blocks. Output bytes are identical to encoding/base64's encoder.
func NewEncoder(enc *Encoding, w io.Writer) io.WriteCloser {
	return &encoder{enc: enc, w: w}
}

type encoder struct {
	err  error
	enc  *Encoding
	w    io.Writer
	buf  [3]byte    // buffered data waiting to be encoded
	nbuf int        // number of bytes in buf
	out  [2048]byte // output buffer; 2048/4*3 = 1536 input bytes per flush
}

func (e *encoder) Write(p []byte) (n int, err error) {
	if e.err != nil {
		return 0, e.err
	}

	// Leading fringe.
	if e.nbuf > 0 {
		var i int
		for i = 0; i < len(p) && e.nbuf < 3; i++ {
			e.buf[e.nbuf] = p[i]
			e.nbuf++
		}
		n += i
		p = p[i:]
		if e.nbuf < 3 {
			return
		}
		e.enc.Encode(e.out[:4], e.buf[:])
		if _, e.err = e.w.Write(e.out[:4]); e.err != nil {
			return n, e.err
		}
		e.nbuf = 0
	}

	// Large interior chunks.
	for len(p) >= 3 {
		nn := len(e.out) / 4 * 3
		if nn > len(p) {
			nn = len(p) - len(p)%3
		}
		no := nn / 3 * 4 // encoded length of this chunk
		e.enc.Encode(e.out[:no], p[:nn])
		if _, e.err = e.w.Write(e.out[:no]); e.err != nil {
			return n, e.err
		}
		n += nn
		p = p[nn:]
	}

	// Trailing fringe.
	copy(e.buf[:], p)
	e.nbuf = len(p)
	n += len(p)
	return
}

// Close flushes any pending output from the encoder. It is an error to
// call Write after calling Close.
func (e *encoder) Close() error {
	if e.err == nil && e.nbuf > 0 {
		size := e.enc.EncodedLen(e.nbuf)
		e.enc.Encode(e.out[:size], e.buf[:e.nbuf])
		e.nbuf = 0
		_, e.err = e.w.Write(e.out[:size])
	}
	return e.err
}

// NewDecoder constructs a new base64 stream decoder. It delegates to
// encoding/base64's decoder, which already filters newlines and handles
// padding across reads; stream reads are typically small enough that SIMD
// would not help.
func NewDecoder(enc *Encoding, r io.Reader) io.Reader {
	return base64.NewDecoder(enc.std, r)
}
