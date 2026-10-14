package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestLoadAndAutoDetect(t *testing.T) {
	dir := t.TempDir()
	proto := `syntax = "proto3"; package test; message Wrong { string other = 9; } message Event { string name = 1; int64 count = 2; }`
	if err := os.WriteFile(filepath.Join(dir, "event.proto"), []byte(proto), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if prefix := r.CommonPackagePrefix(); prefix != "test" {
		t.Fatalf("common package prefix = %q, want test", prefix)
	}
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	b = protowire.AppendString(b, "stars")
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, 42)
	var progress []float64
	md, msgs, err := r.DecodeWithProgress([][]byte{b}, "", func(value float64, _ string) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	if md.FullName() != "test.Event" || len(msgs) != 1 {
		t.Fatalf("selected %s", md.FullName())
	}
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Fatalf("decode progress did not complete: %v", progress)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("decode progress moved backwards: %v", progress)
		}
	}
}

func TestDecodeRequestedMessageNotFound(t *testing.T) {
	r := loadTestSchema(t, `
		syntax = "proto3";
		package test;
		message Event { string name = 1; }
	`)

	md, messages, err := r.Decode([][]byte{{}}, "test.Missing")
	if err == nil || !strings.Contains(err.Error(), `message "test.Missing" not found`) {
		t.Fatalf("Decode() error = %v, want missing-message error", err)
	}
	if md != nil || messages != nil {
		t.Fatalf("Decode() = %v, %v, %v; want nil results", md, messages, err)
	}
}

func TestAutoDetectValidatesPayloadsAfterProbe(t *testing.T) {
	r := loadTestSchema(t, `
		syntax = "proto3";
		package test;
		message AlphaWrong { ZuluChild value = 1; }
		message BravoCorrect { bytes value = 1; }
		message ZuluChild { int32 number = 1; }
	`)

	// The initial probe is ambiguous: empty payloads decode as every candidate. The
	// final payload is valid bytes data, but its contents are not a valid Child message.
	payloads := make([][]byte, 33)
	payloads[32] = protowire.AppendBytes(
		protowire.AppendTag(nil, 1, protowire.BytesType),
		[]byte{0x0f},
	)

	md, messages, err := r.Decode(payloads, "")
	if err != nil {
		t.Fatal(err)
	}
	if md.FullName() != "test.BravoCorrect" {
		t.Fatalf("selected %s, want test.BravoCorrect", md.FullName())
	}
	if len(messages) != len(payloads) {
		t.Fatalf("decoded %d messages, want %d", len(messages), len(payloads))
	}
}

func TestAutoDetectScoresMessageValuedMapContents(t *testing.T) {
	r := loadTestSchema(t, `
		syntax = "proto3";
		package test;
		message AlphaWrong { map<string, WrongValue> items = 1; }
		message BravoCorrect { map<string, CorrectValue> items = 1; }
		message WrongValue { string other = 2; }
		message CorrectValue { string known = 1; }
	`)

	value := protowire.AppendString(
		protowire.AppendTag(nil, 1, protowire.BytesType),
		"signal",
	)
	entry := protowire.AppendString(
		protowire.AppendTag(nil, 1, protowire.BytesType),
		"key",
	)
	entry = protowire.AppendBytes(
		protowire.AppendTag(entry, 2, protowire.BytesType),
		value,
	)
	payload := protowire.AppendBytes(
		protowire.AppendTag(nil, 1, protowire.BytesType),
		entry,
	)

	md, messages, err := r.Decode([][]byte{payload}, "")
	if err != nil {
		t.Fatal(err)
	}
	if md.FullName() != "test.BravoCorrect" || len(messages) != 1 {
		t.Fatalf("selected %s with %d messages", md.FullName(), len(messages))
	}
}

func TestLoadRejectsDuplicateMessageNames(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{
		"first.proto":  `syntax = "proto3"; package test; message Duplicate { string first = 1; }`,
		"second.proto": `syntax = "proto3"; package test; message Duplicate { int32 second = 1; }`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(dir)
	if err == nil {
		t.Fatalf("Load() = %+v, nil; want duplicate-symbol error", result)
	}
	if !strings.Contains(err.Error(), "test.Duplicate") {
		t.Fatalf("Load() error = %q, want duplicate message name", err)
	}
}

func TestLoadDirectoryUsesImportQualifiedFileNames(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"common/v1/common.proto": `
			syntax = "proto3";
			package example.common.v1;
			message Value { string text = 1; }
		`,
		"logs/v1/logs.proto": `
			syntax = "proto3";
			package example.logs.v1;
			import "example/proto/common/v1/common.proto";
			message Record { example.common.v1.Value value = 1; }
		`,
	}
	for name, source := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"example.common.v1.Value": false,
		"example.logs.v1.Record":  false,
	}
	for _, message := range result.Messages {
		if _, ok := want[string(message.FullName())]; ok {
			want[string(message.FullName())] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("message %s was not loaded", name)
		}
	}
}

func loadTestSchema(t *testing.T, source string) Result {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.proto"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
