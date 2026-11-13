# make test                   vet and test everything
# make build                  every release binary in dist/: Linux, macOS, Windows
# make mac                    just the Mac binary, dist/lincos
# make install HOST=<ssh>     build for that server, install the agent and (re)start it

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X main.version=$(VERSION)
GOBUILD  = CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

.PHONY: test build mac install

test:
	go vet ./...
	go test ./...

# The agent runs on Linux; the lincos command (lincos open) on all three
TARGETS = linux-amd64 linux-arm64 darwin-arm64 darwin-amd64 windows-amd64 windows-arm64

build: $(foreach t,$(TARGETS),dist/lincos-$(t)$(if $(findstring windows,$(t)),.exe))
	cp deploy/lincos.service dist/

.PHONY: dist/lincos-%
dist/lincos-%:
	GOOS=$(word 1,$(subst -, ,$(basename $*))) GOARCH=$(word 2,$(subst -, ,$(basename $*))) $(GOBUILD) -o $@ ./cmd/lincos

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
