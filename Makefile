GO ?= go

.PHONY: build run demo test
build:
	$(GO) build -o jira-tui ./cmd/jira-tui
run: build
	./jira-tui
demo: build
	./jira-tui --demo
test:
	$(GO) vet ./... && $(GO) test ./...
