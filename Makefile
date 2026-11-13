# PiDash build, deploy and release.
#
# Development
#   make dev            run backend (-mock) + Vite dev server with HMR
#   make build          build frontend and a native binary into bin/pidash
#   make lint test      static checks and tests
#
# Your own Pi, straight from this checkout (override with PI_HOST=pi@192.168.1.50)
#   make install        first-time install over SSH
#   make deploy         push a new build over SSH and restart the backend
#   make uninstall      remove PiDash from the Pi again
#   make install-local  install on this machine (when working on the Pi itself)
#   Add INSTALL_FLAGS="--autostart --console-apps --phone-input" as needed.
#
# Releases
#   make dist                    build the release packages into dist/
#   make release VERSION=v0.2.0  tag and push; GitHub Actions builds and publishes
#                                the release, which the one-line installer and
#                                every Pi's update.sh then pick up
#   (or, without a checkout: GitHub -> Actions -> Release -> Run workflow)

PI_HOST  ?= pi@pidash.local
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   = -s -w -X main.version=$(VERSION)

INSTALL_FLAGS ?=
APP_DIR   = .local/share/pidash

.PHONY: dev dev-backend dev-web build build-web build-pi deploy install install-local uninstall lint test clean dist release

dev:
	@$(MAKE) -j2 dev-backend dev-web

dev-backend:
	go run ./cmd/pidash -v -mock -listen 127.0.0.1:8080

dev-web:
	cd web && npm run dev

build-web: web/node_modules
	cd web && npm run build

web/node_modules: web/package.json
	cd web && npm install --no-audit --no-fund
	@touch web/node_modules

build: build-web
	go build -ldflags="$(LDFLAGS)" -o bin/pidash ./cmd/pidash

build-pi: build-web
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o bin/pidash-arm64 ./cmd/pidash

deploy: build-pi
	scp bin/pidash-arm64 $(PI_HOST):$(APP_DIR)/pidash.new
	ssh $(PI_HOST) 'mv -f $(APP_DIR)/pidash.new $(APP_DIR)/pidash && systemctl --user restart pidash && systemctl --user --no-pager --lines=5 status pidash'

install: build-pi
	ssh $(PI_HOST) 'mkdir -p /tmp/pidash-install'
	scp -r bin/pidash-arm64 deploy config.example.toml install.sh $(PI_HOST):/tmp/pidash-install/
	ssh -t $(PI_HOST) 'cd /tmp/pidash-install && bash deploy/install.sh $(INSTALL_FLAGS); rc=$$?; cd / && rm -r /tmp/pidash-install; exit $$rc'

install-local: build
	bash deploy/install.sh --binary bin/pidash $(INSTALL_FLAGS)

uninstall:
	ssh $(PI_HOST) 'bash $(APP_DIR)/uninstall.sh'

lint:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)
	cd web && npx tsc --noEmit
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -S warning install.sh deploy/*.sh scripts/*.sh; else echo "shellcheck not installed, skipping the shell scripts"; fi

test:
	go test ./...

dist: build-web
	scripts/dist.sh $(VERSION)

# Publishing is one command: it checks the tree, tags it and pushes the tag.
# The Release workflow on GitHub does the rest.
release:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$$' || (echo "usage: make release VERSION=v0.2.0"; exit 1)
	@test -z "$$(git status --porcelain)" || (echo "commit or stash your changes first"; exit 1)
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || (echo "tag $(VERSION) already exists"; exit 1)
	$(MAKE) lint test
	git tag -a "$(VERSION)" -m "PiDash $(VERSION)"
	git push origin "$(VERSION)"
	@echo
	@echo "Tag $(VERSION) pushed. GitHub Actions is now building the release:"
	@echo "  https://github.com/DarioRomano/PiDash/actions"

clean:
	rm -rf bin dist web/dist/*
	@touch web/dist/.gitkeep
