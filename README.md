# base64

A SIMD-accelerated, 100% compatible drop-in replacement for Go's `encoding/base64`.

```go
import "github.com/xiaost/base64"

s := base64.StdEncoding.EncodeToString(data) // same API as encoding/base64
b, err := base64.URLEncoding.DecodeString(s)
```

The `encoding/base64` API is mirrored for predefined and custom encodings,
buffer helpers, string helpers, and streaming encoders/decoders.
`CorruptInputError` is a type alias of `encoding/base64.CorruptInputError`,
so existing error handling keeps working unchanged.

## How it works

Bulk data is processed by per-architecture assembly kernels.
Everything else — tails, padding, embedded newlines, errors,
custom alphabets the kernels can't handle, CPUs without the needed features —
is delegated to `encoding/base64` itself, so behavior is identical by construction.

| arch  | kernels | alphabet support |
|-------|---------|------------------|
| arm64 | NEON (always available) | any alphabet |
| amd64 | AVX2, else SSSE3 | encode: standard first 62; decode: std/URL |
| other | none | stdlib |

- arm64: 48-byte encode blocks via LD3+TBL+ST4; 64-char decode blocks via LD4+TBL/TBX+ST3.
- amd64: runtime CPUID selects AVX2 or SSSE3; encode blocks are 24 / 12 bytes and decode blocks are 32 / 16 chars.

Small inputs may not enter the kernels.
On arm64, a 32-byte encode is below the 48-byte block size,
and its 44-character output is below the 64-character decode block size.
Those cases mostly measure wrapper and stdlib fallback overhead,
so they can be slower than `encoding/base64`.

The amd64 kernels implement Wojciech Muła's [base64 SIMD algorithms](http://0x80.pl/notesen/2016-01-12-sse-base64-encoding.html);
the arm64 kernels follow the design of [aklomp/base64](https://github.com/aklomp/base64).

Decoding stops the SIMD loop when a 64/32/16-char block contains non-plain alphabet bytes,
such as padding, `\r`, `\n`, or invalid bytes,
and hands the rest to the stdlib.
Newline skipping, strict mode, padding checks, and `CorruptInputError` offsets match the stdlib.
One exception keeps line-wrapped base64 (PEM, MIME) fast:
when the stop leads to a run of newlines at a 4-char quantum boundary,
the decoder scalar-decodes the plain alphabet quanta before the run,
skips the newlines and re-enters the kernel.
Only whole quanta of plain alphabet chars are ever consumed this way —
padding and invalid bytes still go to the stdlib together with everything after them,
whose semantics they depend on.

Build with `-tags purego` to disable all assembly.

## Compatibility testing

Verified against `encoding/base64` by differential tests and fuzzing,
covering predefined and custom/padded/strict encodings, corrupt input,
error values, truncations, embedded newlines and streaming APIs.
All three kernel sets are exercised in CI-style runs: NEON natively on Apple Silicon, SSSE3 under Rosetta 2, and AVX2 inside a QEMU `-cpu max` VM.
Benchmark output is published by GitHub Actions,
so current CI numbers are available at [github.com/xiaost/base64/actions](https://github.com/xiaost/base64/actions).
