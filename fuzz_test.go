package base64

import (
	"bytes"
	"testing"
)

var fuzzPairs = pairs()

func FuzzEncode(f *testing.F) {
	f.Add([]byte(""))
	f.Add([]byte("f"))
	f.Add([]byte("foobar"))
	f.Add(bytes.Repeat([]byte{0xFF, 0x00, 0xA5}, 64))
	f.Fuzz(func(t *testing.T, in []byte) {
		for _, p := range fuzzPairs {
			want := p.std.EncodeToString(in)
			if got := p.our.EncodeToString(in); got != want {
				t.Fatalf("%s: encode mismatch for %q", p.name, in)
			}
		}
	})
}

func FuzzDecode(f *testing.F) {
	f.Add("")
	f.Add("QUJD")
	f.Add("QUJDRA==")
	f.Add("QQ==QQ==")
	f.Add("A\nB\rC=!")
	f.Add(string(bytes.Repeat([]byte("QUJD"), 40)))
	f.Add(string(bytes.Repeat(append(bytes.Repeat([]byte("QUJD"), 16), '\n'), 4)))
	f.Fuzz(func(t *testing.T, in string) {
		for _, p := range fuzzPairs {
			wb, werr := p.std.DecodeString(in)
			gb, gerr := p.our.DecodeString(in)
			if gerr != werr || !bytes.Equal(gb, wb) {
				t.Fatalf("%s: decode mismatch for %q: (%q, %v) vs (%q, %v)",
					p.name, in, gb, gerr, wb, werr)
			}
		}
	})
}
