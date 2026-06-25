// Package simd provides SIMD-accelerated base64 block kernels.
//
// Kernels process whole blocks only and never touch padding, newlines or
// invalid bytes; callers must handle the remainder with encoding/base64.
// A nil *Encoder / *Decoder means the alphabet or CPU is not supported by
// the kernels and the caller must fall back entirely.
package simd
