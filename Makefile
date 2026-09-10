.PHONY: build run demo test vet dumps

build:
	go build -o bin/devopstui ./cmd/devopstui

demo:
	go run ./cmd/devopstui --demo

run:
	go run ./cmd/devopstui

test:
	go test ./...

vet:
	go vet ./...

dumps:
	mkdir -p dumps && DUMP_DIR=$(CURDIR)/dumps go test ./internal/ui/ -run . >/dev/null && ls dumps
