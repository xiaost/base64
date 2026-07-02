//go:build arm64 && !purego

package simd

import (
	stdb64 "encoding/base64"
	"testing"
)

// BenchmarkDecodeKernels compares the nibble-classification kernel used
// for std/URL alphabets against the chained-TBX 256-byte LUT kernel used
// for custom alphabets, on identical std-alphabet input. The nibble
// kernel is the fast path because 4-register TBL/TBX chains are slow on
// most cores (Apple M5 Max: 18.9 vs 11.4 GB/s).
func BenchmarkDecodeKernels(b *testing.B) {
	src := make([]byte, 48<<10)
	for i := range src {
		src[i] = byte(i*131 + 7)
	}
	enc := make([]byte, stdb64.StdEncoding.EncodedLen(len(src)))
	stdb64.StdEncoding.Encode(enc, src)
	n := len(enc) / 64 * 64
	dst := make([]byte, n/4*3)
	var alpha [64]byte
	copy(alpha[:], stdAlphabet)
	d := NewDecoder(&alpha)
	b.Run("nibble", func(b *testing.B) {
		b.SetBytes(int64(n))
		for i := 0; i < b.N; i++ {
			if decodeNEONFast(&dst[0], &enc[0], n, &d.tab[0]) != n {
				b.Fatal("stopped early")
			}
		}
	})
	b.Run("lut256", func(b *testing.B) {
		b.SetBytes(int64(n))
		for i := 0; i < b.N; i++ {
			if decodeNEON(&dst[0], &enc[0], n, &d.lut[0]) != n {
				b.Fatal("stopped early")
			}
		}
	})
}
