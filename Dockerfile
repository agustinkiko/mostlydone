# syntax=docker/dockerfile:1
# Built and run locally for linux/arm64 and linux/amd64 (2026-11-14).
# amd64 was run under emulation on an ARM Mac, not on real amd64 hardware.
# Pick the target with: docker build --platform linux/amd64 -t mostlydone .

# 1. Build the web UI into internal/webui/dist.
#    Build stages run on the build machine's own chip (fast, no emulation);
#    Go cross-compiles for the target chip.
FROM --platform=$BUILDPLATFORM node:24-alpine AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p ../internal/webui/dist && npm run build

# 2. Build a static Go binary with the UI embedded.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS server
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY --from=ui /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/mostlydone ./cmd/mostlydone \
 && mkdir -p /out/data

# 3. Minimal runtime image, non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/mostlydone /mostlydone
COPY --from=server --chown=65532:65532 /out/data /data
ENV MOSTLYDONE_DB=/data/mostlydone.db \
    MOSTLYDONE_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/mostlydone"]
