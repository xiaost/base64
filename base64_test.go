package base64

import (
	"bytes"
	stdb64 "encoding/base64"
	"math/rand"
	"strings"
	"testing"

	"github.com/xiaost/base64/internal/simd"
)

const cryptAlpha = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Pathological alphabets encoding/base64.NewEncoding accepts: '=' as a
// data character (decodeMap beats the padding check in the stdlib's
// decoder) and high bytes.
var (
	equalsAlpha = encodeStd[:63] + "="
	highAlpha   = encodeStd[:62] + "\x80\xFF"
)

type pair struct {
	name string
	our  *Encoding
	std  *stdb64.Encoding
}

func pairs() []pair {
	return []pair{
		{"std", StdEncoding, stdb64.StdEncoding},
		{"url", URLEncoding, stdb64.URLEncoding},
		{"rawstd", RawStdEncoding, stdb64.RawStdEncoding},
		{"rawurl", RawURLEncoding, stdb64.RawURLEncoding},
		{"strict", StdEncoding.Strict(), stdb64.StdEncoding.Strict()},
		{"rawstrict", RawStdEncoding.Strict(), stdb64.RawStdEncoding.Strict()},
		{"custom", NewEncoding(cryptAlpha), stdb64.NewEncoding(cryptAlpha)},
		{"custompad", NewEncoding(cryptAlpha).WithPadding('*'), stdb64.NewEncoding(cryptAlpha).WithPadding('*')},
		{"customraw", NewEncoding(cryptAlpha).WithPadding(NoPadding), stdb64.NewEncoding(cryptAlpha).WithPadding(stdb64.NoPadding)},
		{"equals63", NewEncoding(equalsAlpha), stdb64.NewEncoding(equalsAlpha)},
		{"highbytes", NewEncoding(highAlpha), stdb64.NewEncoding(highAlpha)},
		{"highraw", NewEncoding(highAlpha).WithPadding(NoPadding), stdb64.NewEncoding(highAlpha).WithPadding(stdb64.NoPadding)},
	}
}

// noSIMD returns a copy of e with SIMD disabled, to exercise fallback.
func noSIMD(e *Encoding) *Encoding {
	e2 := *e
	e2.enc, e2.dec = nil, nil
	return &e2
}

func sizes() []int {
	s := make([]int, 0, 200)
	for i := 0; i <= 130; i++ {
		s = append(s, i)
	}
	for _, i := range []int{191, 192, 193, 255, 256, 257, 1023, 1024, 1025, 4096, 1 << 16, 1<<16 + 7} {
		s = append(s, i)
	}
	return s
}

func TestEncodeCompat(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			for _, n := range sizes() {
				src := make([]byte, n)
				r.Read(src)
				want := p.std.EncodeToString(src)
				if got := p.our.EncodeToString(src); got != want {
					t.Fatalf("n=%d: EncodeToString mismatch\ngot  %q\nwant %q", n, got, want)
				}
				if got := noSIMD(p.our).EncodeToString(src); got != want {
					t.Fatalf("n=%d: fallback EncodeToString mismatch", n)
				}
				// Encode must write exactly EncodedLen bytes.
				dst := bytes.Repeat([]byte{0xA5}, p.our.EncodedLen(n)+16)
				p.our.Encode(dst, src)
				if string(dst[:len(want)]) != want {
					t.Fatalf("n=%d: Encode mismatch", n)
				}
				for i := len(want); i < len(dst); i++ {
					if dst[i] != 0xA5 {
						t.Fatalf("n=%d: Encode wrote past EncodedLen at +%d", n, i-len(want))
					}
				}
			}
		})
	}
}

func decodeBoth(t *testing.T, p pair, in string) {
	t.Helper()
	wd := make([]byte, p.std.DecodedLen(len(in))+16)
	gd := make([]byte, len(wd))
	wn, werr := p.std.Decode(wd, []byte(in))
	gn, gerr := p.our.Decode(gd, []byte(in))
	if gn != wn || gerr != werr || !bytes.Equal(gd[:gn], wd[:wn]) {
		t.Fatalf("Decode(%q): got (%d, %v), want (%d, %v)", in, gn, gerr, wn, werr)
	}
	fn, ferr := noSIMD(p.our).Decode(gd, []byte(in))
	if fn != wn || ferr != werr {
		t.Fatalf("fallback Decode(%q): got (%d, %v), want (%d, %v)", in, fn, ferr, wn, werr)
	}
	gb, gerr := p.our.DecodeString(in)
	wb, werr := p.std.DecodeString(in)
	if gerr != werr || !bytes.Equal(gb, wb) {
		t.Fatalf("DecodeString(%q): got (%q, %v), want (%q, %v)", in, gb, gerr, wb, werr)
	}
}

func TestDecodeCompat(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			for _, n := range sizes() {
				src := make([]byte, n)
				r.Read(src)
				decodeBoth(t, p, p.std.EncodeToString(src))
			}
		})
	}
}

func TestDecodeNewlines(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	src := make([]byte, 600)
	r.Read(src)
	for _, p := range pairs() {
		enc := p.std.EncodeToString(src)
		for _, nl := range []string{"\n", "\r\n", "\r"} {
			for _, every := range []int{1, 4, 63, 64, 76} {
				var b strings.Builder
				for i, c := range []byte(enc) {
					if i > 0 && i%every == 0 {
						b.WriteString(nl)
					}
					b.WriteByte(c)
				}
				decodeBoth(t, p, b.String())
				decodeBoth(t, p, nl+enc+nl)
			}
		}
	}
}

func TestDecodeCorrupt(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	src := make([]byte, 300)
	r.Read(src)
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			enc := p.std.EncodeToString(src)
			for i := 0; i < len(enc); i++ {
				// Invalid bytes, pads, cross-alphabet chars ('+' into URL
				// input, '-' into std), and valid-char substitution.
				for _, bad := range []byte{'!', '=', 0x80, 0xFF, ' ', '\t', '+', '/', '-', '_', '*', 'A'} {
					b := []byte(enc)
					b[i] = bad
					decodeBoth(t, p, string(b))
				}
			}
			// Truncations.
			for i := 0; i <= len(enc); i++ {
				decodeBoth(t, p, enc[:i])
			}
		})
	}
}

func TestDecodeVectors(t *testing.T) {
	vectors := []string{
		"", "A", "AA", "AAA", "AAAA", "AAAAA", "AAAAAA",
		"=", "==", "A=", "A==", "AA=", "AA==", "AAA=", "AAAA====",
		"QQ==", "QQ=", "Q===", "A==A", "QQ==QQ==", "QQ==\n", "\nQQ==",
		"Q\n\nQ==", "QUJD", "QUJDRA==", "ab-_", "ab+/",
		strings.Repeat("QUJD", 16), strings.Repeat("QUJD", 16) + "RA==",
		strings.Repeat("QUJD", 16) + "=AAA", strings.Repeat("QUJD", 15) + "QQ==RA==",
		strings.Repeat("A", 64) + "=" + strings.Repeat("A", 63),
		// Non-zero trailing padding bits ('B' = 1): strict mode rejects.
		"QB==", strings.Repeat("QUJD", 16) + "QB==", strings.Repeat("QUJD", 16) + "QUJB",
		// Whitespace-only and padding at SIMD block boundaries.
		strings.Repeat("\n", 200), strings.Repeat("\r\n", 100),
		strings.Repeat("QUJD", 15) + "QQ==", strings.Repeat("QUJD", 16) + "\nQQ==",
		strings.Repeat("QUJD", 16) + "QQ==" + strings.Repeat("QUJD", 16),
		"====" + strings.Repeat("QUJD", 16),
		// Kernel re-entry across newline runs (line-wrapped base64).
		strings.Repeat("QUJD\n", 50),                         // lines shorter than a block
		"QUJD\n" + strings.Repeat("QUJD", 32),                // short first line, long rest
		strings.Repeat(strings.Repeat("QUJD", 19)+"\r\n", 4), // MIME-style 76-char lines
		strings.Repeat("QUJD", 16) + "\n\r\n\n" + strings.Repeat("QUJD", 32) + "\nQQ==\n",
		strings.Repeat("QUJD", 16) + "\n!" + strings.Repeat("QUJD", 16),
		strings.Repeat("QUJD", 16) + "\nQU!D\n" + strings.Repeat("QUJD", 16),
	}
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			for _, v := range vectors {
				decodeBoth(t, p, v)
			}
		})
	}
}

// Wrapped (PEM/MIME-style) input with corruption at every position must
// keep exact stdlib parity through the kernel re-entry path.
func TestDecodeWrappedCorrupt(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	src := make([]byte, 256)
	r.Read(src)
	for _, p := range pairs() {
		t.Run(p.name, func(t *testing.T) {
			enc := p.std.EncodeToString(src)
			for _, nl := range []string{"\n", "\r\n"} {
				var b strings.Builder
				for i := 0; i < len(enc); i += 64 {
					b.WriteString(enc[i:min(i+64, len(enc))])
					b.WriteString(nl)
				}
				wrapped := b.String()
				decodeBoth(t, p, wrapped)
				for i := 0; i < len(wrapped); i++ {
					for _, bad := range []byte{'!', '=', '\n', 0xFF} {
						w := []byte(wrapped)
						w[i] = bad
						decodeBoth(t, p, string(w))
					}
				}
			}
		})
	}
}

func TestRoundTrip1MB(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	src := make([]byte, 1<<20)
	r.Read(src)
	for _, p := range pairs() {
		enc := p.our.EncodeToString(src)
		if want := p.std.EncodeToString(src); enc != want {
			t.Fatalf("%s: 1MB encode mismatch", p.name)
		}
		got, gerr := p.our.DecodeString(enc)
		want, werr := p.std.DecodeString(enc)
		if gerr != werr || !bytes.Equal(got, want) {
			t.Fatalf("%s: 1MB decode parity failed: %v vs %v", p.name, gerr, werr)
		}
		// With '=' in the alphabet even the stdlib does not round-trip
		// (padding decodes as data); for sane alphabets it must.
		if p.name != "equals63" && !bytes.Equal(got, src) {
			t.Fatalf("%s: 1MB round trip failed", p.name)
		}
	}
}

func TestAppend(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	for _, p := range pairs() {
		for _, n := range []int{0, 1, 2, 3, 47, 48, 100, 1000} {
			src := make([]byte, n)
			r.Read(src)
			pre := []byte("prefix")

			want := p.std.AppendEncode(append([]byte{}, pre...), src)
			got := p.our.AppendEncode(append([]byte{}, pre...), src)
			if !bytes.Equal(got, want) {
				t.Fatalf("%s n=%d: AppendEncode mismatch", p.name, n)
			}

			enc := p.std.EncodeToString(src)
			appendDecodeBoth(t, p, pre, enc)

			// Corrupt input must keep dst/err parity too.
			if len(enc) > 10 {
				bad := []byte(enc)
				bad[len(bad)/2] = '!'
				appendDecodeBoth(t, p, nil, string(bad))
			}
		}
	}
}

func panics(f func()) (p bool) {
	defer func() { p = recover() != nil }()
	f()
	return false
}

// appendDecodeBoth checks AppendDecode parity, including panic parity:
// with '=' in the alphabet, the stdlib's own AppendDecode under-allocates
// (it strips trailing '=' as padding although it decodes as data) and
// panics; ours must do exactly the same.
func appendDecodeBoth(t *testing.T, p pair, pre []byte, enc string) {
	t.Helper()
	var wd, gd []byte
	var werr, gerr error
	wp := panics(func() { wd, werr = p.std.AppendDecode(append([]byte{}, pre...), []byte(enc)) })
	gp := panics(func() { gd, gerr = p.our.AppendDecode(append([]byte{}, pre...), []byte(enc)) })
	if gp != wp {
		t.Fatalf("%s: AppendDecode panic mismatch: %v vs %v", p.name, gp, wp)
	}
	if !wp && (gerr != werr || !bytes.Equal(gd, wd)) {
		t.Fatalf("%s: AppendDecode mismatch: (%q, %v) vs (%q, %v)", p.name, gd, gerr, wd, werr)
	}
}

// Invalid constructor arguments must panic exactly like the stdlib.
func TestConstructorPanics(t *testing.T) {
	for _, alpha := range []string{
		"", "short", encodeStd + "x", // wrong length
		encodeStd[:63] + "\n", encodeStd[:63] + "\r", // newline
		encodeStd[:63] + "A", // duplicate symbol
	} {
		if !panics(func() { stdb64.NewEncoding(alpha) }) {
			t.Fatalf("stdlib NewEncoding(%q) did not panic", alpha)
		}
		if !panics(func() { NewEncoding(alpha) }) {
			t.Fatalf("NewEncoding(%q) did not panic", alpha)
		}
	}
	for _, pad := range []rune{'\n', '\r', 'A', '+', rune(0x100), -2} {
		if !panics(func() { stdb64.StdEncoding.WithPadding(pad) }) {
			t.Fatalf("stdlib WithPadding(%q) did not panic", pad)
		}
		if !panics(func() { StdEncoding.WithPadding(pad) }) {
			t.Fatalf("WithPadding(%q) did not panic", pad)
		}
	}
}

// A too-short dst is a contract violation that panics in the stdlib; it
// must panic here too, whether or not the SIMD path is engaged.
func TestShortDstPanics(t *testing.T) {
	src := make([]byte, 300)
	for _, p := range pairs() {
		enc := []byte(p.std.EncodeToString(src))
		// dst 10: all-scalar; dst 70/50: SIMD consumes blocks first.
		for _, m := range []int{0, 10, 70} {
			if !panics(func() { p.std.Encode(make([]byte, m), src) }) {
				t.Fatalf("%s dst=%d: stdlib Encode did not panic", p.name, m)
			}
			if !panics(func() { p.our.Encode(make([]byte, m), src) }) {
				t.Fatalf("%s dst=%d: Encode did not panic", p.name, m)
			}
		}
		for _, m := range []int{0, 10, 50} {
			if !panics(func() { p.std.Decode(make([]byte, m), enc) }) {
				t.Fatalf("%s dst=%d: stdlib Decode did not panic", p.name, m)
			}
			if !panics(func() { p.our.Decode(make([]byte, m), enc) }) {
				t.Fatalf("%s dst=%d: Decode did not panic", p.name, m)
			}
		}
	}
}

// Decoding into a dst sized exactly to the output (which is smaller than
// DecodedLen for padded input) must work like the stdlib.
func TestDecodeExactDst(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for _, p := range pairs() {
		for _, n := range []int{1, 46, 47, 48, 49, 95, 96, 97, 300} {
			src := make([]byte, n)
			r.Read(src)
			enc := []byte(p.std.EncodeToString(src))
			want := make([]byte, n)
			got := make([]byte, n)
			var wn, gn int
			var werr, gerr error
			// For equals63 even the stdlib panics here: padding '='
			// decodes as data and overflows the exact-size dst.
			wp := panics(func() { wn, werr = p.std.Decode(want, enc) })
			gp := panics(func() { gn, gerr = p.our.Decode(got, enc) })
			if gp != wp {
				t.Fatalf("%s n=%d: exact-dst panic mismatch: %v vs %v", p.name, n, gp, wp)
			}
			if !wp && (gn != wn || gerr != werr || !bytes.Equal(got[:gn], want[:wn])) {
				t.Fatalf("%s n=%d: exact-dst decode mismatch: (%d, %v) vs (%d, %v)",
					p.name, n, gn, gerr, wn, werr)
			}
		}
	}
}

func TestSIMDActive(t *testing.T) {
	t.Logf("kernels: %q", simd.Active())
	t.Logf("std enc=%v dec=%v", StdEncoding.enc != nil, StdEncoding.dec != nil)
	t.Logf("url enc=%v dec=%v", URLEncoding.enc != nil, URLEncoding.dec != nil)
	t.Logf("custom enc=%v dec=%v",
		NewEncoding(cryptAlpha).enc != nil, NewEncoding(cryptAlpha).dec != nil)
}

// When a kernel set is active, the predefined std/URL encodings must engage
// it. A typo in the alphabet constants (encodeStd/encodeURL here or
// stdAlphabet/urlAlphabet in internal/simd) would make NewEncoder/NewDecoder
// return nil and silently fall back to the stdlib for the whole library; the
// differential tests stay green because the fallback also matches the stdlib.
// This asserts the fast path is actually wired up.
func TestSIMDEngaged(t *testing.T) {
	if simd.Active() == "" {
		t.Skip("no SIMD kernels on this platform")
	}
	for _, p := range []struct {
		name string
		e    *Encoding
	}{
		{"std", StdEncoding}, {"url", URLEncoding},
		{"rawstd", RawStdEncoding}, {"rawurl", RawURLEncoding},
	} {
		if p.e.enc == nil {
			t.Errorf("%s: encoder fell back to stdlib (SIMD %q active)", p.name, simd.Active())
		}
		if p.e.dec == nil {
			t.Errorf("%s: decoder fell back to stdlib (SIMD %q active)", p.name, simd.Active())
		}
	}
}
