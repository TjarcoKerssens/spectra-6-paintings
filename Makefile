BINARY  := spectra6
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= $(HOME)

.PHONY: all build test race lint fmt vet cover install clean run frame-config frame-compile frame-upload

all: fmt vet test build

build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/spectra6

test:
	go test ./...

# Race detector on, because the preview renders on a background goroutine while
# the UI keeps handling keys.
race:
	go test -race ./...

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -l -w .

vet:
	go vet ./...

# Optional, only if you have it installed.
lint:
	golangci-lint run

install: build
	install -d $(PREFIX)/bin
	install -m 0755 bin/$(BINARY) $(PREFIX)/bin/$(BINARY)
	@echo "installed to $(PREFIX)/bin/$(BINARY)"

clean:
	rm -rf bin coverage.out

run: build
	./bin/$(BINARY)

# The frame's firmware. Settings come from .env, see .env.example.
ENV := set -a && . ./.env && set +a && cd frame

frame-config:
	$(ENV) && esphome config ee02.yaml

frame-compile:
	$(ENV) && esphome compile ee02.yaml

frame-upload: frame-compile
	$(ENV) && esphome upload ee02.yaml --device "$$FRAME_DEVICE"
