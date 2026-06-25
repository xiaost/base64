# Repository Guidelines

## Project
Single-package Go module `github.com/xiaost/base64`: a SIMD-accelerated
`encoding/base64` mirror.

- Root files (`base64.go`, `encode.go`, `decode.go`, `stream.go`): public API and stdlib fallback.
- `internal/simd`: arch front ends and assembly kernels.
- `*_test.go`: differential tests, fuzz targets, benchmarks.
- `.github/workflows/unittest.yml`: tests and vet.
- `.github/workflows/benchmark.yml`: benchmarks on amd64 and arm64 Linux runners.

## Commands
```sh
go test -timeout 60s ./...
go test -tags purego -timeout 60s ./...
go vet ./...
go test -timeout 60s -run 'TestDecodeCompat/url' -v
go test -timeout 60s -fuzz FuzzDecode -fuzztime 30s
go test -run '^$' -bench . -benchtime=1s -timeout 60s ./...
```

## Invariants
Match `encoding/base64` exactly: results, errors, and panics.

- SIMD consumes only whole blocks of plain alphabet bytes.
- Tails, padding, newlines, corrupt input, unsupported alphabets, and unsupported CPUs fall back to `encoding/base64`.
- `CorruptInputError` aliases the stdlib error; rebase offsets after a SIMD-consumed prefix.
- `AppendEncode`, `AppendDecode`, `EncodeToString`, `DecodeString`, and stream APIs keep stdlib parity, including panics.
- `purego` disables all assembly and must pass the same tests.

## Architecture
- `Encoding` wraps `*base64.Encoding` plus optional SIMD encoder/decoder.
- `nil` SIMD fields mean "use stdlib for the whole operation".
- arm64: NEON, any alphabet.
- amd64: runtime AVX2 or SSSE3; encode supports alphabets sharing the standard first 62 chars; decode supports standard and URL alphabets.
- `NewDecoder` intentionally delegates stream decoding to the stdlib.

## Tests
Prefer differential tests against `encoding/base64` over golden-only tests.

- Use `pairs()` for the encoding matrix and `noSIMD()` for fallback paths.
- Around encode/decode boundary changes, cover SIMD edges: 48/64 on arm64, 16/32 on amd64.
- Around kernels or CPU selection, run native tests, `-tags purego`, and benchmarks; also test amd64 via Rosetta/QEMU or GitHub Actions when possible.
- Keep unit tests short and targeted; place them in `_test.go` next to the behavior.

## Changes
Keep commits narrowly scoped, especially around assembly and public API parity.
