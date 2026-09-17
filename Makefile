.PHONY: build run demo test vet dumps shots shots-svg check

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

# shots drives the real binary in a tmux pty and captures the screen, so it
# catches anything the test harness's View() dumps miss (colour, cursor,
# terminal-level layout). See scripts/shots.sh for the scene list.
shots: build
	./scripts/shots.sh

shots-svg: build
	./scripts/shots.sh --svg

# What CI runs, and what to run before pushing.
check: vet test
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi
