IMAGE ?= helio:local
SOURCE_DATE_EPOCH ?= 0

.PHONY: build container container-down container-up hardware-test test test-e2e web

build: web
	go build ./cmd/helio

container:
	docker build --pull --build-arg SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) -t $(IMAGE) .

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

web:
	npm --prefix web run build
