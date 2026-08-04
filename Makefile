.PHONY: run tidy vendor build bump-version lint test test-postgres-integrity check-translations check-duplicates psql-migrate psql-status psql-reset validate-db backup-db

GO_CMD = go
GORELEASER_CMD = goreleaser
GOLANGCI_LINT_CMD = golangci-lint

# PostgreSQL Migration Variables
PSQL_SCRIPT = scripts/migrate_psql.sh

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

test-postgres-integrity:
	$(GO_CMD) test -tags testtools -v -race -p 1 -count=1 -timeout 10m \
		-run '^(TestAllModulesRoundTripEveryMeaningfulField|TestImportChatDataRollsBackEarlierModules|TestImportWarnsCreatesMissingParents|TestLegacyBackupPreservesFieldsThatVersionDidNotExport|TestDeleteCaptchaAttemptByIDAtomicSingleClaim|TestCreateMutedUserUpdatesExistingSchedule|TestCreateMutedUserConcurrentUpsert|TestCreateCaptchaAttemptReplacesExistingChallenge|TestCreateCaptchaAttemptIfEnabledRejectsDisabledChat|TestCaptchaAttemptClaimsSchedulePermissionRestore|TestDeleteMutedUserIfUnchangedPreservesNewerSchedule|TestIncrementCaptchaAttemptsRejectsRefreshedChallenge|TestUpdateChannelClearsAndReassignsNormalizedUsername|TestConcurrentConnectKeepsOneRowPerUser|TestAddFilterConcurrentInsert|TestAddNoteConcurrentInsert|TestGetUserReportSettings_Defaults|TestConcurrentReportBlockListUpdates|TestWarnUserCreatesMissingParentRows|TestConcurrentWarnAndRemovePreserveCount)$$' \
		./alita/db/backup ./alita/db/captcha ./alita/db/channels ./alita/db/connections \
		./alita/db/filters ./alita/db/notes ./alita/db/reports ./alita/db/warns

check-translations:
	@echo "🔍 Checking for missing translations..."
	@cd scripts/check_translations && $(GO_CMD) mod tidy && $(GO_CMD) run main.go

check-duplicates:
	@echo "🔍 Checking for duplicate code..."
	@which $(GOLANGCI_LINT_CMD) > /dev/null || (echo "golangci-lint not found, install it from https://golangci-lint.run/usage/install/" && exit 1)
	$(GOLANGCI_LINT_CMD) run --enable dupl

# PostgreSQL Migration Targets
psql-migrate:
	@echo "🚀 Applying PostgreSQL migrations..."
	@bash $(PSQL_SCRIPT)

psql-status:
	@echo "📊 PostgreSQL Migration Status"
	@if [ -z "$${PSQL_DB_HOST:-}" ] || [ -z "$${PSQL_DB_NAME:-}" ] || [ -z "$${PSQL_DB_USER:-}" ]; then \
		echo "❌ Error: Required environment variables not set"; \
		echo "   Please set: PSQL_DB_HOST, PSQL_DB_NAME, PSQL_DB_USER"; \
		exit 1; \
	fi
	@echo "🔍 Checking migration status..."
	@PGPASSWORD="$${PSQL_DB_PASSWORD:-}" PGSSLMODE="$${PSQL_DB_SSLMODE:-prefer}" psql -h "$$PSQL_DB_HOST" -p "$${PSQL_DB_PORT:-5432}" -U "$$PSQL_DB_USER" -d "$$PSQL_DB_NAME" -c \
		"SELECT version, executed_at FROM schema_migrations ORDER BY executed_at DESC;"

psql-reset:
	@if [ -z "$${PSQL_DB_HOST:-}" ] || [ -z "$${PSQL_DB_NAME:-}" ] || [ -z "$${PSQL_DB_USER:-}" ]; then \
		echo "❌ Error: Required environment variables not set"; \
		echo "   Please set: PSQL_DB_HOST, PSQL_DB_NAME, PSQL_DB_USER"; \
		exit 1; \
	fi
	@echo "🔥 WARNING: This will DROP ALL TABLES in the database!"
	@echo "   Database: $$PSQL_DB_NAME on $$PSQL_DB_HOST"
	@echo "   Type 'yes' to confirm: " && read confirm && [ "$$confirm" = "yes" ] || (echo "Cancelled" && exit 1)
	@echo "💣 Resetting database..."
	@PGPASSWORD="$${PSQL_DB_PASSWORD:-}" PGSSLMODE="$${PSQL_DB_SSLMODE:-prefer}" psql -h "$$PSQL_DB_HOST" -p "$${PSQL_DB_PORT:-5432}" -U "$$PSQL_DB_USER" -d "$$PSQL_DB_NAME" -c \
		"DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
	@echo "✅ Database reset complete"

# Database validation and backup
validate-db:
	@echo "🔍 Validating database for orphaned records..."
	@go run scripts/validate_orphaned_data.go

backup-db:
	@echo "💾 Creating database backup..."
	@./scripts/backup_database.sh
