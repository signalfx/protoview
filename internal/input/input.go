package input

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/klauspost/compress/zstd"
)

const (
	maxDecompressedSize = 64 << 20
	maxFrameCount       = 10_000
)

var errDecompressedSizeLimit = errors.New("decompressed input exceeds size limit")
var errFrameCountLimit = errors.New("framed input exceeds payload count limit")

type Data struct {
	Payloads    [][]byte
	Compression string
	Framing     string
}

func Read(path string) (Data, error) {
	return ReadWithProgress(path, nil)
}

func ReadWithProgress(path string, progress func(float64)) (Data, error) {
	raw, err := readFile(path, func(value float64) { report(progress, value*.5) })
	if err != nil {
		return Data{}, err
	}
	decoded, compression, err := decompress(raw, func(value float64) { report(progress, .5+value*.5) })
	if err != nil {
		return Data{}, err
	}
	payloads, framing, err := splitFrames(decoded)
	if err != nil {
		return Data{}, err
	}
	report(progress, 1)
	return Data{Payloads: payloads, Compression: compression, Framing: framing}, nil
}

func readFile(path string, progress func(float64)) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	buffer := make([]byte, 256*1024)
	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			output.Write(buffer[:n])
			if info.Size() > 0 {
				progress(float64(output.Len()) / float64(info.Size()))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, readErr
		}
	}
	progress(1)
	return output.Bytes(), nil
}

func decompress(b []byte, progress func(float64)) ([]byte, string, error) {
	return decompressWithLimit(b, maxDecompressedSize, progress)
}

func decompressWithLimit(b []byte, limit int, progress func(float64)) ([]byte, string, error) {
	if limit <= 0 {
		return nil, "none", fmt.Errorf("decompression size limit must be positive")
	}
	if len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b {
		compressed := &countingReader{reader: bytes.NewReader(b)}
		r, err := gzip.NewReader(compressed)
		if err != nil {
			return nil, "gzip", err
		}
		defer r.Close()
		out, err := readAll(r, compressed, len(b), limit, progress)
		if err != nil {
			return nil, "gzip", fmt.Errorf("decompress gzip: %w", err)
		}
		return out, "gzip", nil
	}
	if len(b) >= 4 && isZstdMagic(binary.LittleEndian.Uint32(b[:4])) {
		compressed := &countingReader{reader: bytes.NewReader(b)}
		r, err := zstd.NewReader(compressed, zstd.WithDecoderMaxMemory(uint64(limit)+1))
		if err != nil {
			return nil, "zstd", err
		}
		defer r.Close()
		out, err := readAll(r, compressed, len(b), limit, progress)
		if err != nil {
			if errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
				err = fmt.Errorf("%w of %d bytes", errDecompressedSizeLimit, limit)
			}
			return nil, "zstd", fmt.Errorf("decompress zstd: %w", err)
		}
		return out, "zstd", nil
	}
	report(progress, 1)
	return b, "none", nil
}

type countingReader struct {
	reader io.Reader
	read   int
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += n
	return n, err
}

func readAll(reader io.Reader, compressed *countingReader, compressedSize, limit int, progress func(float64)) ([]byte, error) {
	var output bytes.Buffer
	buffer := make([]byte, 256*1024)
	for {
		remaining := limit - output.Len()
		readSize := 1
		if remaining >= len(buffer) {
			readSize = len(buffer)
		} else {
			readSize = remaining + 1
		}
		n, err := reader.Read(buffer[:readSize])
		if n > 0 {
			if n > limit-output.Len() {
				return nil, fmt.Errorf("%w of %d bytes", errDecompressedSizeLimit, limit)
			}
			output.Write(buffer[:n])
			if compressedSize > 0 {
				report(progress, float64(compressed.read)/float64(compressedSize))
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	report(progress, 1)
	return output.Bytes(), nil
}

func report(progress func(float64), value float64) {
	if progress != nil {
		progress(min(1, max(0, value)))
	}
}

func isZstdMagic(magic uint32) bool {
	return magic == 0xfd2fb528 || magic&0xfffffff0 == 0x184d2a50
}

func splitFrames(b []byte) ([][]byte, string, error) {
	for _, candidate := range []struct {
		name  string
		order binary.ByteOrder
	}{
		{"32-bit big-endian", binary.BigEndian}, {"32-bit little-endian", binary.LittleEndian},
	} {
		frames := make([][]byte, 0, min(len(b)/4, maxFrameCount))
		frameCount := 0
		for pos := 0; pos < len(b); {
			if len(b)-pos < 4 {
				frames = nil
				break
			}
			n := uint64(candidate.order.Uint32(b[pos : pos+4]))
			pos += 4
			if n > uint64(len(b)-pos) {
				frames = nil
				break
			}
			frameCount++
			if frameCount <= maxFrameCount {
				frames = append(frames, b[pos:pos+int(n)])
			}
			pos += int(n)
		}
		if frames != nil && frameCount > maxFrameCount {
			return nil, candidate.name, fmt.Errorf("%w of %d", errFrameCountLimit, maxFrameCount)
		}
		if len(frames) > 0 {
			return frames, candidate.name, nil
		}
	}
	return [][]byte{b}, "none", nil
}
