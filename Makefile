.PHONY: build db-start db-stop migrate verify-schema check-contract test-go test-web test-e2e backup-verify preview eval-live

build:
	go build -o ./bin/server ./cmd/server
	go build -o ./bin/migrate ./cmd/migrate
	go build -o ./bin/eval-live ./cmd/eval-live
	npm --prefix web run build

db-start:
	./scripts/dev-db.sh start

db-stop:
	./scripts/dev-db.sh stop

migrate: db-start
	go build -o ./bin/migrate ./cmd/migrate
	./bin/migrate up

verify-schema: db-start
	./bin/migrate verify

check-contract:
	npm --prefix web run check:contract

test-go: db-start
	go test -v -race -count=1 ./...

test-web:
	npm --prefix web run test

test-e2e: build
	npm --prefix web run test:e2e

backup-verify: build
	./scripts/verify-backup-restore.sh

preview: build
	./scripts/start-preview.sh

eval-live:
	go run ./cmd/eval-live --fixtures ./tests/fixtures/eval_scenarios.json --limit 4
