# Multi-stage build for the OmniHub gateway.
#
# Stage 1 compiles a static Go binary; stage 2 ships only the binary
# plus the small set of files it actually needs at runtime
# (CA certificates for HTTPS upstream calls, tzdata for timestamps,
# busybox wget for the HEALTHCHECK probe). Final image ≈ 30–40 MB.
#
# Build with versioned metadata:
#   docker build \
#       --build-arg VERSION=$(git describe --tags --always --dirty) \
#       --build-arg COMMIT=$(git rev-parse --short HEAD) \
#       --build-arg DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
#       -t ghcr.io/jami1024/omnihub:latest .

# --- Frontend stage -------------------------------------------------
# Builds the React admin UI; output lands at /web/dist and is copied
# into /src/internal/web/dist for the Go embed directive in the next
# stage. Cached on package*.json so day-to-day Go-only changes do not
# re-run npm.
FROM node:22-alpine AS frontend

WORKDIR /web

# Copy only the manifest first so the npm-install layer is cached on
# dependency changes alone. `npm install` regenerates the lockfile if
# absent; switch to `npm ci` once package-lock.json is committed.
COPY web/package.json ./
RUN npm install

COPY web/ ./
RUN npm run build

# --- Build stage ----------------------------------------------------
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Dependency layer: pulled before source so rebuilds skip the network
# when go.mod / go.sum are unchanged.
COPY go.mod go.sum ./
RUN go mod download

# Source.
COPY . .

# Replace the placeholder dist/ with the real frontend build before
# go build picks up the embed directive.
# Vite's outDir = "../internal/web/dist" resolves from /web to /internal,
# so the bundle lands at /internal/web/dist in the frontend image.
COPY --from=frontend /internal/web/dist /src/internal/web/dist

# Build args populated by docker build --build-arg (see header).
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux \
    go build \
        -trimpath \
        -ldflags "-s -w \
            -X main.version=${VERSION} \
            -X main.commit=${COMMIT} \
            -X main.date=${DATE}" \
        -o /out/omnihub \
        ./cmd/omnihub

# --- Runtime stage --------------------------------------------------
FROM alpine:3.20

# ca-certificates : TLS to api.anthropic.com / aws-external-anthropic
# tzdata          : non-UTC timestamps in logs when TZ is set
# wget (busybox)  : HEALTHCHECK probe
RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -S omnihub && \
    adduser -S -G omnihub -H -h /app omnihub

COPY --from=builder /out/omnihub /usr/local/bin/omnihub

USER omnihub
WORKDIR /app

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/healthz || exit 1

# OCI labels link the image back to the source repo on GitHub
# Container Registry and surface licence info on hub UIs.
LABEL org.opencontainers.image.title="OmniHub" \
      org.opencontainers.image.description="Commercial-grade unified AI gateway." \
      org.opencontainers.image.source="https://github.com/jami1024/omnihub" \
      org.opencontainers.image.licenses="MIT"

ENTRYPOINT ["omnihub"]
