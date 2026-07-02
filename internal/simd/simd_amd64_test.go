//go:build amd64 && !purego

package simd

import (
	stdb64 "encoding/base64"
	"testing"
)

func TestEncodeConsumesFinalBlock(t *testing.T) {
	if !hasAVX2 && !hasSSSE3 {
		t.Skip("no amd64 SIMD support")
	}
	var alpha [64]byte
	copy(alpha[:], stdAlphabet)
	src := make([]byte, 24)
	for i := range src {
		src[i] = byte(i*17 + 3)
	}
	want := make([]byte, stdb64.StdEncoding.EncodedLen(len(src)))
	stdb64.StdEncoding.Encode(want, src)

	if hasAVX2 {
		testEncodeConsumesFinalBlock(t, &alpha, src, want, 24, 32)
	}
	if hasSSSE3 {
		avx2 := hasAVX2
		hasAVX2 = false
		defer func() { hasAVX2 = avx2 }()
		testEncodeConsumesFinalBlock(t, &alpha, src[:12], want[:16], 12, 16)
	}
}

func testEncodeConsumesFinalBlock(t *testing.T, alpha *[64]byte, src, want []byte, ns, nd int) {
	t.Helper()
	e := NewEncoder(alpha)
	if e == nil {
		t.Fatal("NewEncoder returned nil")
	}
	dst := make([]byte, nd)
	gotD, gotS := e.Encode(dst, src)
	if gotD != nd || gotS != ns {
		t.Fatalf("Encode returned nd=%d ns=%d, want nd=%d ns=%d", gotD, gotS, nd, ns)
	}
	if string(dst) != string(want) {
		t.Fatalf("Encode mismatch: got %q want %q", dst, want)
	}
}
