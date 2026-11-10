BINARY  := create-bootable-usb
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
EXE     := $(shell go env GOEXE)
DIST    := dist
TARGETS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.DEFAULT_GOAL := help
.PHONY: help build run list test test-race test-integration vet fmt cross snapshot release clean

help: ## Show the available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build for this computer into bin/
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY)$(EXE) .

run: build ## Build and start the writer (ARGS=path/to/image.iso is optional)
	./bin/$(BINARY)$(EXE) $(ARGS)

list: build ## Build and list detected devices (no admin rights needed)
	./bin/$(BINARY)$(EXE) --list $(ARGS)

test: ## Run the unit tests
	go test ./...

test-race: ## Run the unit tests with the race detector
	go test -race ./...

test-integration: ## macOS only: write and verify a RAM disk through /dev/rdisk
	go test -tags integration -count=1 -v ./internal/burn/

vet: ## Vet the code for every release platform
	@for t in $(TARGETS); do \
	    echo "go vet $$t"; \
	    GOOS=$${t%/*} GOARCH=$${t#*/} go vet ./... || exit 1; \
	done

fmt: ## Format the code
	gofmt -s -w .

cross: ## Build the binaries for every release platform into dist/
	@mkdir -p $(DIST)
	@for t in $(TARGETS); do \
	    os=$${t%/*}; arch=$${t#*/}; ext=""; \
	    if [ "$$os" = windows ]; then ext=.exe; fi; \
	    out=$(DIST)/$(BINARY)_$${os}_$${arch}$$ext; \
	    echo "building $$out"; \
	    CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done

snapshot: ## Build the release archives locally with GoReleaser (nothing is uploaded)
	goreleaser release --snapshot --clean

release: ## Tag and push a version, e.g. make release V=v0.0.2; GitHub Actions publishes it
	@test -n "$(V)" || { echo "usage: make release V=v0.0.2"; exit 1; }
	git tag -a $(V) -m "$(V)"
	git push origin $(V)

clean: ## Remove build output
	rm -rf bin $(DIST)
