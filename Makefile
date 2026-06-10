# ==============================================================================
# Load environment variables 
ifneq (,$(wildcard ./.env))
include .env
export
endif

# ==============================================================================
# Define dependencies
GOLANG          := golang:1.25
ALPINE          := alpine:3.22

POSTGRES        := postgres:17.2
SERVICE_APP    	:= relays-api
BASE_IMAGE_NAME := insidious000
VERSION         := 0.0.1
API_IMAGE       := $(BASE_IMAGE_NAME)/$(SERVICE_APP):$(VERSION)

# ==============================================================================
# Main

run:
	lsof -i :10267 | awk 'NR!=1 {print $$2}' | xargs -r kill -9
	go run ./cmd/api/main.go

# ==============================================================================
# Modules support

deps-reset:
	git checkout -- go.mod
	go mod tidy
	go mod vendor

tidy:
	go mod tidy
	go mod vendor

deps-list:
	go list -m -u -mod=readonly all

deps-upgrade:
	go get -u -v ./...
	go mod tidy
	go mod vendor

deps-cleancache:
	go clean -modcache

verify-checksums:
	go mod verify

list:
	go list -mod=mod all

lint:
	CGO_ENABLED=0 go vet ./...
	staticcheck -go 1.25.0 -checks=all ./...

staticcheck:
	staticcheck -go 1.25.0 -checks=all ./...

# ==============================================================================
# Database migrations

# Create empty migration manually
migrate-create:
	@read -p "Enter migration name: " name; \
	migrate create -ext sql -dir internal/sdk/migrate/sql -seq $$name

# Run migrations
migrate-up:
	migrate -path internal/sdk/migrate/sql -database "$(DATABASE_URL)?sslmode=$(or $(BLUEPRINT_DB_SSLMODE),require)" up

migrate-down:
	migrate -path internal/sdk/migrate/sql -database "$(DATABASE_URL)?sslmode=$(or $(BLUEPRINT_DB_SSLMODE),require)" down

migrate-force:
	@read -p "Enter version: " version; \
	migrate -path internal/sdk/migrate/sql -database "$(DATABASE_URL)?sslmode=$(or $(BLUEPRINT_DB_SSLMODE),require)" force $$version

migrate-version:
	migrate -path internal/sdk/migrate/sql -database "$(DATABASE_URL)?sslmode=$(or $(BLUEPRINT_DB_SSLMODE),require)" version

migrate-drop:
	migrate -path internal/sdk/migrate/sql -database "$(DATABASE_URL)?sslmode=$(or $(BLUEPRINT_DB_SSLMODE),require)" drop -f


# ==============================================================================

# go version -m $(which staticcheck) | head -n 1 | awk '{print $NF}'

revert:
	git reset --hard HEAD~1