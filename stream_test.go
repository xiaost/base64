package base64

import (
	"bytes"
	stdb64 "encoding/base64"
	"errors"
	"io"
	"math/rand"
	"testing"
)

func TestStreamEncoder(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for _, p := range pairs() {
		for _, n := range []int{0, 1, 2, 3, 4, 100, 1535, 1536, 1537, 5000} {
			src := make([]byte, n)
			r.Read(src)
			for _, chunk := range []int{1, 2, 3, 7, 64, 1000, n + 1} {
				var got, want bytes.Buffer

				w := NewEncoder(p.our, &got)
				sw := stdb64.NewEncoder(p.std, &want)
				for i := 0; i < n; i += chunk {
					end := min(i+chunk, n)
					if _, err := w.Write(src[i:end]); err != nil {
						t.Fatal(err)
					}
					if _, err := sw.Write(src[i:end]); err != nil {
						t.Fatal(err)
					}
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if err := sw.Close(); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got.Bytes(), want.Bytes()) {
					t.Fatalf("%s n=%d chunk=%d: encoder output mismatch", p.name, n, chunk)
				}
			}
		}
	}
}

type errWriter struct{ n int }

func (w *errWriter) Write(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, errors.New("boom")
	}
	w.n--
	return len(p), nil
}

func TestStreamEncoderError(t *testing.T) {
	w := NewEncoder(StdEncoding, &errWriter{n: 1})
	if _, err := w.Write(make([]byte, 4096)); err == nil {
		t.Fatal("expected write error")
	}
	if _, err := w.Write([]byte("x")); err == nil {
		t.Fatal("expected sticky error")
	}
	if err := w.Close(); err == nil {
		t.Fatal("expected sticky error on close")
	}
}

func TestStreamDecoder(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	src := make([]byte, 4000)
	r.Read(src)
	for _, p := range pairs() {
		enc := p.our.EncodeToString(src)
		got, gerr := io.ReadAll(NewDecoder(p.our, bytes.NewReader([]byte(enc))))
		want, werr := io.ReadAll(stdb64.NewDecoder(p.std, bytes.NewReader([]byte(enc))))
		if gerr != werr || !bytes.Equal(got, want) {
			t.Fatalf("%s: stream decode parity mismatch: %v vs %v", p.name, gerr, werr)
		}
		// Sane alphabets must round-trip ('=' in the alphabet does not,
		// even in the stdlib).
		if p.name != "equals63" && (gerr != nil || !bytes.Equal(got, src)) {
			t.Fatalf("%s: stream decode mismatch: %v", p.name, gerr)
		}
	}
}
