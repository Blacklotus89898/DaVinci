# build must work from PowerShell, where make runs recipes through cmd.exe:
# no `mkdir -p`, no `find` (cmd's FIND searches text), no `2>/dev/null`, and
# Windows binaries need .exe — the MCP registration runs knowledge-service.exe.
ifeq ($(OS),Windows_NT)
EXE := .exe
endif

BINDIR   := bin
SERVICE  := $(BINDIR)/knowledge-service$(EXE)
CLI      := $(BINDIR)/knowledge$(EXE)
DB_PATH  ?= knowledge.db
DOCS_PATH ?= docs
VERSION  ?= $(or $(shell git describe --tags --always --dirty),dev)
LIB_SRC  := $(wildcard internal/*/*.go)

.PHONY: build install ingest clean tidy lint test

build: $(SERVICE) $(CLI)

$(BINDIR):
	mkdir $(BINDIR)

$(SERVICE): go.sum $(wildcard cmd/knowledge-service/*.go mcp/*.go) $(LIB_SRC) | $(BINDIR)
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $@ ./cmd/knowledge-service

$(CLI): go.sum $(wildcard cmd/knowledge/*.go) $(LIB_SRC) | $(BINDIR)
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o $@ ./cmd/knowledge

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
