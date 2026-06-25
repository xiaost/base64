//go:build (!amd64 && !arm64) || purego

package simd

// Active returns the name of the kernel set in use, for diagnostics.
func Active() string { return "" }

// Minimum src lengths below which Encode / Decode are no-ops.
const (
	MinEncode = 0
	MinDecode = 0
)

// Encoder is unavailable on this platform.
type Encoder struct{}

// Decoder is unavailable on this platform.
type Decoder struct{}

func NewEncoder(*[64]byte) *Encoder { return nil }
func NewDecoder(*[64]byte) *Decoder { return nil }

func (*Encoder) Encode(dst, src []byte) (nd, ns int) { return 0, 0 }
func (*Decoder) Decode(dst, src []byte) (nd, ns int) { return 0, 0 }
