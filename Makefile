.PHONY: build hardware-test test test-e2e web

build: web
	go build ./cmd/helio

test:
	go test ./...

test-e2e:
	npm --prefix web run test:e2e

hardware-test:
	go run ./cmd/helio-hardware-test

web:
	npm --prefix web run build
