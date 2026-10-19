GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := $(CURDIR)/bin/tools/golangci-lint-$(GOLANGCI_LINT_VERSION)/golangci-lint

.PHONY: run build fmt fmt-check install-lint lint vet test coverage check clean
run:
	go run ./cmd/server
build:
	go build -trimpath -o bin/server ./cmd/server
fmt:
	gofmt -w cmd internal
fmt-check:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
# Install the official release binary locally; the installer verifies SHA-256.
$(GOLANGCI_LINT):
	@mkdir -p "$(@D)"
	@set -eu; installer=$$(mktemp); trap 'rm -f "$$installer"' EXIT; \
		curl -fsSL "https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh" -o "$$installer"; \
		sh "$$installer" -b "$(@D)" "$(GOLANGCI_LINT_VERSION)"
install-lint: $(GOLANGCI_LINT)
lint: install-lint
	"$(GOLANGCI_LINT)" run --config .golangci.yml ./...
vet:
	go vet ./...
test:
	go test -race ./...
coverage:
	go test -count=1 -race -coverpkg=./... -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
	@go tool cover -func=coverage.out | awk '/^total:/ { if ($$3 + 0 < 80) { print "Coverage must be at least 80%"; exit 1 } }'
check: fmt-check lint vet coverage build
clean:
	rm -rf bin coverage.out coverage.html
