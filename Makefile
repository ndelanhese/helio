.PHONY: build hardware-test test web

build: web
	go build ./cmd/helio

test:
	go test ./...

hardware-test:
	go run ./cmd/helio-hardware-test

web:
	npm --prefix web run build
