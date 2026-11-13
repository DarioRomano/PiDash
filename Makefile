# PiDash build & deploy.
#   make dev            run backend (-mock) + Vite dev server with HMR
#   make build          build frontend and a native binary into bin/pidash
#   make build-pi       cross-compile for the Pi (linux/arm64) into bin/pidash-arm64
#
# From another machine, over SSH (override the address with PI_HOST=pi@192.168.1.50):
#   make install        first-time install on the Pi's existing desktop
#   make deploy         push a new build and restart the backend
#   make uninstall      remove PiDash from the Pi again
#
# Directly on the Pi (needs Go 1.22+ and Node 18+ on the Pi):
#   make install-local
#
# Add INSTALL_FLAGS=--autostart to open the dashboard whenever the desktop starts.

PI_HOST  ?= pi@pidash.local
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   = -s -w -X main.version=$(VERSION)

INSTALL_FLAGS ?=
APP_DIR   = .local/share/pidash

.PHONY: dev dev-backend dev-web build build-web build-pi deploy install install-local uninstall lint test clean

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
	scp -r bin/pidash-arm64 deploy config.example.toml $(PI_HOST):/tmp/pidash-install/
	ssh -t $(PI_HOST) 'cd /tmp/pidash-install && bash deploy/install.sh $(INSTALL_FLAGS); rc=$$?; cd / && rm -r /tmp/pidash-install; exit $$rc'

install-local: build
	bash deploy/install.sh --binary bin/pidash $(INSTALL_FLAGS)

uninstall:
	ssh $(PI_HOST) 'bash $(APP_DIR)/uninstall.sh'

lint:
	go vet ./...
	cd web && npx tsc --noEmit

test:
	go test ./...

clean:
	rm -rf bin web/dist/*
	@touch web/dist/.gitkeep
