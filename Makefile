IMAGE ?= helio:local

.PHONY: build container container-down container-up hardware-test smoke test test-e2e web workflow-check workflow-contract

build: web
	go build ./cmd/helio

container:
	docker build --pull -t $(IMAGE) .

container-up:
	HELIO_IMAGE=$(IMAGE) docker compose up -d --build

container-down:
	docker compose down

test:
	go test ./...

test-e2e:
	npm --prefix web run test:e2e

hardware-test:
	go run ./cmd/helio-hardware-test

smoke: container
	HELIO_IMAGE=$(IMAGE) ./scripts/smoke-cleanup-test.sh
	HELIO_IMAGE=$(IMAGE) ./scripts/smoke.sh

web:
	npm --prefix web run build

workflow-contract:
	ruby scripts/workflow_contract_test.rb
	ruby scripts/release_preflight_test.rb
	ruby scripts/finalize_release_aliases_test.rb
	ruby scripts/validate_e2e_artifacts_test.rb

workflow-check: workflow-contract
	docker run --rm -v "$(CURDIR):/repo" -w /repo rhysd/actionlint:1.7.12@sha256:b1934ee5f1c509618f2508e6eb47ee0d3520686341fec936f3b79331f9315667 -color
