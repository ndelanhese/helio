.PHONY: build test web

build: web
	go build ./cmd/helio

test:
	go test ./...

web:
	npm --prefix web run build
