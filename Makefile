GO ?= go
VERSION ?= dev
PLATFORMS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build run demo test dist clean
build:
	$(GO) build -ldflags "-X main.version=$(VERSION)" -o jira-tui ./cmd/jira-tui
run: build
	./jira-tui
demo: build
	./jira-tui --demo
test:
	$(GO) vet ./... && $(GO) test ./...

# dist builds the release archives (jira-tui_<os>_<arch>.tar.gz) and checksums.txt, as install.sh expects.
dist:
	rm -rf dist && mkdir -p dist
	set -e; for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; d=dist/$$os-$$arch; mkdir -p $$d; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $$d/jira-tui ./cmd/jira-tui; \
	  tar -czf dist/jira-tui_$${os}_$${arch}.tar.gz -C $$d jira-tui; \
	  rm -rf $$d; \
	done
	cd dist && (sha256sum *.tar.gz 2>/dev/null || shasum -a 256 *.tar.gz) > checksums.txt

clean:
	rm -rf dist jira-tui
