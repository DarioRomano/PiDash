# PiDash build & deploy.
#   make dev        run backend (-mock) + Vite dev server with HMR
#   make build      build frontend and a native binary into bin/pidash
#   make build-pi   cross-compile for the Pi (linux/arm64) into bin/pidash-arm64
#   make deploy     build-pi, copy to the Pi over SSH and restart the service
#   make install    first-time install on the Pi: deploy + run deploy/install.sh
#
# Override the Pi address with PI_HOST=pi@192.168.1.50

PI_HOST  ?= pi@pidash.local
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   = -s -w -X main.version=$(VERSION)

.PHONY: dev dev-backend dev-web build build-web build-pi deploy install lint test clean

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
	scp bin/pidash-arm64 $(PI_HOST):/tmp/pidash
	ssh $(PI_HOST) 'sudo install -m 0755 /tmp/pidash /opt/pidash/pidash && rm /tmp/pidash && sudo systemctl restart pidash && systemctl --no-pager --lines=5 status pidash'

install: build-pi
	ssh $(PI_HOST) 'mkdir -p /tmp/pidash-install'
	scp -r bin/pidash-arm64 deploy config.example.toml $(PI_HOST):/tmp/pidash-install/
	ssh $(PI_HOST) 'cd /tmp/pidash-install && sudo bash deploy/install.sh && rm -rf /tmp/pidash-install'

lint:
	go vet ./...
	cd web && npx tsc --noEmit

test:
	go test ./...

clean:
	rm -rf bin web/dist/*
	@touch web/dist/.gitkeep
