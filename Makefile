.PHONY: run tidy vendor build bump-version lint test check-translations check-duplicates validate-db

GO_CMD = go
GORELEASER_CMD = goreleaser
GOLANGCI_LINT_CMD = golangci-lint

run:
	$(GO_CMD) run main.go

tidy:
	$(GO_CMD) mod tidy

vendor:
	$(GO_CMD) mod vendor

build:
	$(GORELEASER_CMD) release --snapshot --skip=publish --clean --skip=sign

bump-version:
	@if [ -z "$(TAG)" ]; then \
		echo "❌ Error: TAG is required, e.g. make bump-version TAG=v2.19.4"; \
		exit 1; \
	fi
	@bash scripts/bump_version.sh $(TAG)

lint:
	@which $(GOLANGCI_LINT_CMD) > /dev/null || (echo "golangci-lint not found, install it from https://golangci-lint.run/usage/install/" && exit 1)
	$(GOLANGCI_LINT_CMD) run

test:
	$(GO_CMD) test -tags testtools -v -race -coverprofile=coverage.out -coverpkg=$$(go list ./... | grep -v -E '(^github.com/divkix/Alita_Robot$$|scripts/)' | paste -sd, -) -count=1 -timeout 10m ./...

check-translations:
	@echo "🔍 Checking for missing translations..."
	@cd scripts/check_translations && $(GO_CMD) mod tidy && $(GO_CMD) run main.go

check-duplicates:
	@echo "🔍 Checking for duplicate code..."
	@which $(GOLANGCI_LINT_CMD) > /dev/null || (echo "golangci-lint not found, install it from https://golangci-lint.run/usage/install/" && exit 1)
	$(GOLANGCI_LINT_CMD) run --enable dupl

# Database validation
validate-db:
	@echo "🔍 Validating database for orphaned records..."
	@go run scripts/validate_orphaned_data.go
