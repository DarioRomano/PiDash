# syntax=docker/dockerfile:1

# PiDash in a container: the dashboard, for any browser.
#
# A container has no desktop, no TV on an HDMI cable and no sound of its own,
# so this is PiDash run with -standalone: the photos, the time, the weather,
# the calendar, the radio and the settings page, shown by whatever browser
# opens it. Apps, the TV's remote over HDMI, Bluetooth and the update button
# need PiDash installed on the computer itself (see install.sh).
#
#   docker build -t pidash .
#   docker run -d -p 8080:8080 -v pidash:/data -v /path/to/pictures:/photos:ro pidash
#
# Everything PiDash keeps is under /data: the settings saved on its settings
# page, an optional config.toml, and its caches.

# The pages, built on the machine that builds the image, whatever it is for.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# The program, cross-compiled there too: it is plain Go, without C.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/pidash ./cmd/pidash \
 && mkdir -p /out/data

# Nothing but the program, certificates and time zones; not run as root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pidash /pidash
COPY --from=build --chown=65532:65532 /out/data /data
ENV HOME=/data \
    XDG_CONFIG_HOME=/data \
    XDG_DATA_HOME=/data/share \
    XDG_CACHE_HOME=/data/cache \
    PIDASH_PHOTOS=/photos
VOLUME /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=6s --start-period=10s --retries=3 \
    CMD ["/pidash", "-healthcheck", "-listen", ":8080"]
ENTRYPOINT ["/pidash", "-standalone", "-listen", ":8080", "-config", "/data/config.toml"]
