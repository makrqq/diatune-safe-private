GO ?= /workspace/.go/go/bin/go
GOFMT ?= /workspace/.go/go/bin/gofmt
GOCACHE ?= /workspace/.gocache
GOMODCACHE ?= /workspace/.gomodcache
GOTMPDIR ?= /workspace/.gotmp
TMPDIR ?= /workspace/.gotmp

GOENV = TMPDIR=$(TMPDIR) GOTMPDIR=$(GOTMPDIR) GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE)

.PHONY: deps test build run analyze api bot worker fmt

deps:
	mkdir -p $(GOCACHE) $(GOMODCACHE) $(GOTMPDIR)
	$(GOENV) $(GO) mod tidy

fmt:
	find cmd internal -name '*.go' -print0 | xargs -0 $(GOFMT) -w

test:
	$(GOENV) $(GO) test ./...

build:
	$(GOENV) $(GO) build -o bin/diatune-safe ./cmd/diatune-safe

run:
	$(GOENV) $(GO) run ./cmd/diatune-safe

analyze:
	$(GOENV) $(GO) run ./cmd/diatune-safe analyze --patient-id demo --days 14 --synthetic

api:
	$(GOENV) $(GO) run ./cmd/diatune-safe api --host 0.0.0.0 --port 8080

bot:
	$(GOENV) $(GO) run ./cmd/diatune-safe bot

worker:
	$(GOENV) $(GO) run ./cmd/diatune-safe worker
