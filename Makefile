# make test                   vet and test everything
# make build                  binaries for Linux (amd64, arm64) and macOS (arm64) in dist/
# make mac                    just the Mac binary, dist/lincos
# make install HOST=<ssh>     build for that server, install the agent and (re)start it

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
GOBUILD  = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: test build mac install dist/lincos-linux-amd64 dist/lincos-linux-arm64 dist/lincos-darwin-arm64

test:
	go vet ./...
	go test ./...

build: dist/lincos-linux-amd64 dist/lincos-linux-arm64 dist/lincos-darwin-arm64

dist/lincos-linux-amd64:
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $@ ./cmd/lincos
dist/lincos-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $@ ./cmd/lincos
dist/lincos-darwin-arm64:
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $@ ./cmd/lincos

mac:
	$(GOBUILD) -o dist/lincos ./cmd/lincos

# Copies the binary under a new name and moves it into place, so it can replace
# one that is running. Needs ssh as root (or a user who can write those paths).
install:
	@test -n "$(HOST)" || { echo "usage: make install HOST=<ssh host>"; exit 1; }
	@arch=$$(ssh $(HOST) uname -m); \
	case $$arch in x86_64) a=amd64 ;; aarch64) a=arm64 ;; *) echo "unsupported architecture $$arch"; exit 1 ;; esac; \
	$(MAKE) --no-print-directory dist/lincos-linux-$$a && \
	scp -q dist/lincos-linux-$$a $(HOST):/usr/local/bin/lincos.new && \
	scp -q deploy/lincos.service $(HOST):/etc/systemd/system/lincos.service && \
	ssh $(HOST) 'chmod 755 /usr/local/bin/lincos.new && mv /usr/local/bin/lincos.new /usr/local/bin/lincos && \
	  systemctl daemon-reload && systemctl enable lincos 2>/dev/null && systemctl restart lincos && \
	  echo "lincos $$(lincos version | cut -d" " -f2) is $$(systemctl is-active lincos) on $$(hostname)"'
