# Contributing to ProtoView

Thanks for contributing. Keep changes focused, explain user-visible behavior in the
pull request, and include tests for behavior changes where practical.

## Development setup

Install the Go version declared in `go.mod`. The OTLP example tests use the
`opentelemetry-proto` Git submodule, so initialize submodules after a non-recursive
clone:

```sh
git submodule update --init --recursive
```

Build the command with:

```sh
make build
```

Run the Go test suite with:

```sh
make test
```

Run the build and test suite together for local verification with:

```sh
make all
```

Generate a coverage summary and an HTML report with:

```sh
make coverage
```

The generated `coverage.out` and `coverage.html` files are ignored by Git.
