.PHONY: test test-go test-web test-sdk build up up-production down smoke smoke-telemetry smoke-worker-termination load-smoke rehearse-backup-restore validate-oidc-provider

test: test-go test-web test-sdk

test-go:
	docker run --rm -v "$(PWD):/src" -w /src golang:1.24-alpine go test ./...

test-web:
	cd web && npm test

test-sdk:
	cd sdk/javascript && npm test

build:
	docker compose build

up:
	docker compose up --build

up-production:
	docker compose -f compose.yaml -f compose.production-data.yaml up --build

down:
	docker compose down

smoke:
	./scripts/smoke.sh

smoke-telemetry:
	./scripts/smoke-telemetry.sh

smoke-worker-termination:
	./scripts/smoke-worker-termination.sh

load-smoke:
	./scripts/load-smoke.sh

rehearse-backup-restore:
	./scripts/rehearse-backup-restore.sh

validate-oidc-provider:
	./scripts/validate-oidc-provider.sh
