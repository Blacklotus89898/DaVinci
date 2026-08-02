BINDIR   := bin
SERVICE  := $(BINDIR)/knowledge-service
CLI      := $(BINDIR)/knowledge
DB_PATH  ?= knowledge.db
DOCS_PATH ?= docs

.PHONY: build install ingest clean tidy lint test

build: $(SERVICE) $(CLI)

$(SERVICE): go.sum $(shell find cmd/knowledge-service internal mcp -name '*.go' 2>/dev/null)
	@mkdir -p $(BINDIR)
	go build -ldflags="-s -w" -o $@ ./cmd/knowledge-service

$(CLI): go.sum $(shell find cmd/knowledge internal -name '*.go' 2>/dev/null)
	@mkdir -p $(BINDIR)
	go build -ldflags="-s -w" -o $@ ./cmd/knowledge

go.sum: go.mod
	go mod tidy

install: build
	@mkdir -p ~/.local/bin
	cp $(SERVICE) ~/.local/bin/knowledge-service
	cp $(CLI)     ~/.local/bin/knowledge
	@echo "Installed to ~/.local/bin/"

ingest: $(CLI)
	DB_PATH=$(DB_PATH) DOCS_PATH=$(DOCS_PATH) $(CLI) ingest $(DOCS_PATH)

clean:
	rm -rf $(BINDIR) $(DB_PATH)

tidy:
	go mod tidy

lint:
	golangci-lint run ./...

test:
	go test -race ./...
