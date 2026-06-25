package base64

import (
	stdb64 "encoding/base64"
	"fmt"
	"math/rand"
	"testing"
)

var benchSizes = []int{32, 256, 1 << 10, 8 << 10, 64 << 10, 1 << 20}

func label(n int) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%dM", n>>20)
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%dK", n>>10)
	}
	return fmt.Sprintf("%dB", n)
}

func BenchmarkEncode(b *testing.B) {
	r := rand.New(rand.NewSource(42))
	for _, n := range benchSizes {
		src := make([]byte, n)
		r.Read(src)
		dst := make([]byte, StdEncoding.EncodedLen(n))
		b.Run("simd/"+label(n), func(b *testing.B) {
			b.SetBytes(int64(n))
			for i := 0; i < b.N; i++ {
				StdEncoding.Encode(dst, src)
			}
		})
		b.Run("std/"+label(n), func(b *testing.B) {
			b.SetBytes(int64(n))
			for i := 0; i < b.N; i++ {
				stdb64.StdEncoding.Encode(dst, src)
			}
		})
	}
}

func BenchmarkDecodeWrapped(b *testing.B) {
	r := rand.New(rand.NewSource(44))
	src := make([]byte, 1<<20)
	r.Read(src)
	enc := []byte(StdEncoding.EncodeToString(src))
	for _, c := range []struct {
		name string
		n    int
		nl   string
	}{{"pem64", 64, "\n"}, {"mime76", 76, "\r\n"}} {
		var in []byte
		for i := 0; i < len(enc); i += c.n {
			in = append(in, enc[i:min(i+c.n, len(enc))]...)
			in = append(in, c.nl...)
		}
		dst := make([]byte, StdEncoding.DecodedLen(len(in)))
		b.Run("simd/"+c.name, func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				if _, err := StdEncoding.Decode(dst, in); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("std/"+c.name, func(b *testing.B) {
			b.SetBytes(int64(len(in)))
			for i := 0; i < b.N; i++ {
				if _, err := stdb64.StdEncoding.Decode(dst, in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecode(b *testing.B) {
	r := rand.New(rand.NewSource(43))
	for _, n := range benchSizes {
		src := make([]byte, n)
		r.Read(src)
		enc := []byte(StdEncoding.EncodeToString(src))
		dst := make([]byte, StdEncoding.DecodedLen(len(enc)))
		b.Run("simd/"+label(n), func(b *testing.B) {
			b.SetBytes(int64(len(enc)))
			for i := 0; i < b.N; i++ {
				if _, err := StdEncoding.Decode(dst, enc); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("std/"+label(n), func(b *testing.B) {
			b.SetBytes(int64(len(enc)))
			for i := 0; i < b.N; i++ {
				if _, err := stdb64.StdEncoding.Decode(dst, enc); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
