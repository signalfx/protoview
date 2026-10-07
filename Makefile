.PHONY: all build test coverage

COVERAGE_PROFILE ?= coverage.out
COVERAGE_HTML ?= coverage.html

all: build test

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -o bin/protoview .

test:
	go test ./...

coverage:
	go test -coverprofile=$(COVERAGE_PROFILE) ./...
	go tool cover -func=$(COVERAGE_PROFILE)
	go tool cover -html=$(COVERAGE_PROFILE) -o $(COVERAGE_HTML)
	@echo "HTML coverage report written to $(COVERAGE_HTML)"
