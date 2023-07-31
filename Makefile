.PHONY: $(shell ls)

default: test

build: $(shell find -name '*.go') go.sum go.mod
	go install ./...
	go build .

test: build
	go test ./... -v

generate: generate-fixtures generate-gh-pages

generate-fixtures:
	go test ./... -generate

generate-gh-pages: build
	etc/generate-gh-pages
