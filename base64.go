// Package base64 implements base64 encoding as specified by RFC 4648.
//
// It is a drop-in replacement for the standard library's encoding/base64
// package, accelerated with SIMD kernels (NEON on arm64, AVX2 on amd64).
// Results, errors and panics match encoding/base64 exactly: the SIMD
// kernels only process whole blocks of plain alphabet bytes, and
// everything else (tails, padding, newlines, errors, unsupported CPUs or
// alphabets) is handled by encoding/base64 itself.
package base64

import (
	"encoding/base64"

	"github.com/xiaost/base64/internal/simd"
)

const (
	StdPadding rune = base64.StdPadding // Standard padding character
	NoPadding  rune = base64.NoPadding  // No padding
)

const (
	encodeStd = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	encodeURL = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
)

// CorruptInputError is an alias of encoding/base64.CorruptInputError;
// errors returned by this package match the standard library's exactly.
type CorruptInputError = base64.CorruptInputError

// An Encoding is a radix 64 encoding/decoding scheme, defined by a
// 64-character alphabet. It mirrors encoding/base64.Encoding.
type Encoding struct {
	std  *base64.Encoding
	enc  *simd.Encoder // nil: alphabet or CPU unsupported, use std
	dec  *simd.Decoder // nil: alphabet or CPU unsupported, use std
	dmap [256]byte     // char -> 6-bit value; 0xFF marks non-alphabet bytes
	pad  rune
}

// NewEncoding returns a new padded Encoding defined by the given alphabet,
// which must be a 64-byte string that does not contain the padding
// character or CR / LF ('\r', '\n'). Like encoding/base64.NewEncoding, it
// panics on an invalid alphabet.
func NewEncoding(encoder string) *Encoding {
	return wrap(base64.NewEncoding(encoder), encoder, StdPadding)
}

func wrap(std *base64.Encoding, alphabet string, pad rune) *Encoding {
	var a [64]byte
	copy(a[:], alphabet)
	e := &Encoding{std: std, enc: simd.NewEncoder(&a), dec: simd.NewDecoder(&a), pad: pad}
	for i := range e.dmap {
		e.dmap[i] = 0xFF
	}
	// Last occurrence wins, matching encoding/base64's decodeMap.
	for i, c := range a {
		e.dmap[c] = byte(i)
	}
	return e
}

// WithPadding creates a duplicate of enc updated with a specified padding
// character, or NoPadding to disable padding. Like encoding/base64, it
// panics if the padding character is '\r', '\n', not a single byte, or
// part of the alphabet.
func (e *Encoding) WithPadding(padding rune) *Encoding {
	e2 := *e
	e2.std = e.std.WithPadding(padding)
	e2.pad = padding
	return &e2
}

// Strict creates a duplicate of enc updated with strict decoding enabled:
// trailing padding bits must be zero.
func (e *Encoding) Strict() *Encoding {
	e2 := *e
	e2.std = e.std.Strict()
	return &e2
}

var (
	// StdEncoding is the standard base64 encoding, as defined in RFC 4648.
	StdEncoding = NewEncoding(encodeStd)
	// URLEncoding is the alternate base64 encoding defined in RFC 4648,
	// typically used in URLs and file names.
	URLEncoding = NewEncoding(encodeURL)
	// RawStdEncoding is the standard unpadded base64 encoding.
	RawStdEncoding = StdEncoding.WithPadding(NoPadding)
	// RawURLEncoding is the unpadded alternate base64 encoding.
	RawURLEncoding = URLEncoding.WithPadding(NoPadding)
)

// EncodedLen returns the length in bytes of the base64 encoding of an
// input buffer of length n.
func (e *Encoding) EncodedLen(n int) int { return e.std.EncodedLen(n) }

// DecodedLen returns the maximum length in bytes of the decoded data
// corresponding to n bytes of base64-encoded data.
func (e *Encoding) DecodedLen(n int) int { return e.std.DecodedLen(n) }
