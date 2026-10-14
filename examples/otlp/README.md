# OTLP examples

These payloads contain length-prefixed OTLP protobuf messages compressed with
Zstandard. View the metrics example with:

```sh
make build
./bin/protoview examples/otlp/testdata/metrics.pb.zst examples/otlp/opentelemetry-proto
```

The protobuf definitions come from the official `opentelemetry-proto` Git
submodule. If it is empty, initialize it from the repository root:

```sh
git submodule update --init --recursive
```
