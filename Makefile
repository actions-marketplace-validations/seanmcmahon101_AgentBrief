VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/seanmcmahon101/AgentBrief/internal/cli.Version=$(VERSION)

.PHONY: build test lint fmt check install clean golden golden-update

build: ## Build ./agentbrief
	go build -trimpath -ldflags "$(LDFLAGS)" -o agentbrief ./cmd/agentbrief

test: ## Run all tests
	go test ./...

lint: ## go vet and gofmt check
	go vet ./...
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: make fmt" && exit 1)

fmt: ## Format code
	gofmt -w .

check: lint test ## Everything CI runs
	go run ./cmd/agentbrief check

install: ## Install to $GOPATH/bin
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/agentbrief

clean:
	rm -rf agentbrief dist

golden: ## Diff current output against testdata/golden (corpus required)
	scripts/golden.sh diff

golden-update: ## Rewrite testdata/golden from current output
	scripts/golden.sh update
