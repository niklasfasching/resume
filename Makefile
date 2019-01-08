.PHONY: default
default: test build

.PHONY: install
install:
	go get -t ./...

.PHONY: build
build: install
	go build .

.PHONY: test
test: install
	go test ./... -v

.PHONY: generate
generate: generate-fixtures generate-gh-pages

.PHONY: generate-fixtures
generate-fixtures: install
	go test ./... -generate

.PHONY: generate-gh-pages
generate-gh-pages: build
	etc/generate-gh-pages
