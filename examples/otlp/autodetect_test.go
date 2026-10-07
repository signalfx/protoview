package otlp_test

import (
	"path/filepath"
	"testing"

	"github.com/signalfx/protoview/internal/input"
	"github.com/signalfx/protoview/internal/schema"
)

func TestRootMessageAutodetection(t *testing.T) {
	definitions, err := schema.Load("opentelemetry-proto")
	if err != nil {
		t.Fatal(err)
	}
	if prefix := definitions.CommonPackagePrefix(); prefix != "opentelemetry.proto" {
		t.Fatalf("common package prefix = %q, want opentelemetry.proto", prefix)
	}

	fixtures := []struct {
		name    string
		message string
	}{
		{"logs.pb.zst", "opentelemetry.proto.logs.v1.LogsData"},
		{"metrics.pb.zst", "opentelemetry.proto.metrics.v1.MetricsData"},
		{"traces.pb.zst", "opentelemetry.proto.trace.v1.TracesData"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			payload, err := input.Read(filepath.Join("testdata", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			if payload.Compression != "zstd" || payload.Framing == "none" || len(payload.Payloads) < 2 {
				t.Fatalf("unexpected container detection: compression=%s framing=%s payloads=%d", payload.Compression, payload.Framing, len(payload.Payloads))
			}

			md, messages, err := definitions.Decode(payload.Payloads, "")
			if err != nil {
				t.Fatal(err)
			}
			if got := string(md.FullName()); got != fixture.message {
				t.Fatalf("detected message = %q, want %q", got, fixture.message)
			}
			if len(messages) != len(payload.Payloads) {
				t.Fatalf("decoded %d of %d payloads", len(messages), len(payload.Payloads))
			}
		})
	}
}
