package input

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestReadCompressionAndFraming(t *testing.T) {
	for _, tc := range []struct {
		name        string
		encode      func([]byte) []byte
		compression string
	}{
		{"plain", func(b []byte) []byte { return b }, "none"},
		{"gzip", gzipBytes, "gzip"},
		{"zstd", zstdBytes, "zstd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			framed := frame(binary.BigEndian, []byte{8, 1}, []byte{16, 2, 24, 3})
			path := filepath.Join(t.TempDir(), "data")
			if err := os.WriteFile(path, tc.encode(framed), 0600); err != nil {
				t.Fatal(err)
			}
			var progress []float64
			got, err := ReadWithProgress(path, func(value float64) { progress = append(progress, value) })
			if err != nil {
				t.Fatal(err)
			}
			if got.Compression != tc.compression || got.Framing != "32-bit big-endian" || len(got.Payloads) != 2 {
				t.Fatalf("got %+v", got)
			}
			if len(progress) == 0 || progress[len(progress)-1] != 1 {
				t.Fatalf("progress did not complete: %v", progress)
			}
			for i := 1; i < len(progress); i++ {
				if progress[i] < progress[i-1] {
					t.Fatalf("progress moved backwards: %v", progress)
				}
			}
		})
	}
}

func TestFramingMustConsumeEntireInput(t *testing.T) {
	b := append(frame(binary.LittleEndian, []byte{1, 2, 3}), 9)
	got, kind, err := splitFrames(b)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "none" || len(got) != 1 || !bytes.Equal(got[0], b) {
		t.Fatalf("incorrectly detected framing")
	}
}

func TestFramingPayloadCountLimit(t *testing.T) {
	atLimit := make([]byte, maxFrameCount*4)
	got, kind, err := splitFrames(atLimit)
	if err != nil {
		t.Fatalf("split at limit: %v", err)
	}
	if kind != "32-bit big-endian" || len(got) != maxFrameCount {
		t.Fatalf("split at limit = kind %q, %d payloads", kind, len(got))
	}

	_, _, err = splitFrames(append(atLimit, make([]byte, 4)...))
	if !errors.Is(err, errFrameCountLimit) {
		t.Fatalf("split above limit error = %v", err)
	}
	want := "framed input exceeds payload count limit of 10000"
	if err.Error() != want {
		t.Fatalf("split above limit error = %q, want %q", err, want)
	}
}

func TestDecompressionSizeLimit(t *testing.T) {
	const limit = 2048
	for _, tc := range []struct {
		name   string
		encode func([]byte) []byte
	}{
		{"gzip", gzipBytes},
		{"zstd", zstdBytes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			atLimit := bytes.Repeat([]byte("a"), limit)
			got, _, err := decompressWithLimit(tc.encode(atLimit), limit, nil)
			if err != nil {
				t.Fatalf("decompress at limit: %v", err)
			}
			if !bytes.Equal(got, atLimit) {
				t.Fatalf("decompressed data differs at limit")
			}

			_, _, err = decompressWithLimit(tc.encode(append(atLimit, 'a')), limit, nil)
			if !errors.Is(err, errDecompressedSizeLimit) {
				t.Fatalf("decompress above limit error = %v", err)
			}
			want := "decompress " + tc.name + ": decompressed input exceeds size limit of 2048 bytes"
			if err.Error() != want {
				t.Fatalf("decompress above limit error = %q, want %q", err, want)
			}
		})
	}
}

func TestMalformedAndTruncatedCompressedInput(t *testing.T) {
	gzipData := gzipBytes([]byte("payload"))
	zstdData := zstdBytes([]byte("payload"))
	tests := []struct {
		name        string
		data        []byte
		compression string
	}{
		{"malformed gzip", []byte{0x1f, 0x8b, 0x08}, "gzip"},
		{"truncated gzip", gzipData[:len(gzipData)-4], "gzip"},
		{"malformed zstd", []byte{0x28, 0xb5, 0x2f, 0xfd, 0xff}, "zstd"},
		{"truncated zstd", zstdData[:len(zstdData)-1], "zstd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, compression, err := decompress(tc.data, nil)
			if err == nil {
				t.Fatalf("decompress() = %x, %q, nil; want an error", got, compression)
			}
			if compression != tc.compression {
				t.Fatalf("compression = %q, want %q", compression, tc.compression)
			}
		})
	}
}

func frame(order binary.ByteOrder, payloads ...[]byte) []byte {
	var b bytes.Buffer
	for _, p := range payloads {
		_ = binary.Write(&b, order, uint32(len(p)))
		b.Write(p)
	}
	return b.Bytes()
}
func gzipBytes(b []byte) []byte {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	_, _ = w.Write(b)
	_ = w.Close()
	return out.Bytes()
}
func zstdBytes(b []byte) []byte {
	w, _ := zstd.NewWriter(nil)
	defer w.Close()
	return w.EncodeAll(b, nil)
}
