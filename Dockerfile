# Build stage - use golang with Debian for glibc compatibility
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates gcc libc6-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
COPY third_party/ third_party/
COPY cmd/ cmd/
COPY delivery/ delivery/
COPY docs/ docs/
COPY domain/ domain/
COPY infra/ infra/
COPY usecases/ usecases/
COPY brand/ brand/
# pkg holds shared libraries (e.g. vozko/pkg/webhookauth) imported by the server.
COPY pkg/ pkg/
# docs holds the generated swagger package (docs/docs.go), imported as vozko/docs
# by delivery/http/router.go. Only docs.go survives .dockerignore.
COPY docs/ docs/

RUN go mod tidy && go mod download

# Pure-Go build (no cgo dependencies).
# Use -p 4 to limit parallel compilation and prevent memory overflow with Go 1.25
RUN CGO_ENABLED=0 GOOS=linux GOMAXPROCS=4 go build -p 4 -ldflags="-s -w" -o /server ./cmd/server

# Runtime stage
FROM debian:bookworm-slim

# Install minimal runtime dependencies including ffmpeg and tesseract-ocr
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    libstdc++6 \
    wget \
    ffmpeg \
    tesseract-ocr \
    tesseract-ocr-por \
    tesseract-ocr-eng \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /server /server

COPY infra/notifications/templates /infra/notifications/templates

# Copy static assets (keyboard background sound, logos) — resolved relative to WORKDIR "/"
COPY assets /assets

# Create recordings directory with proper permissions
RUN mkdir -p /recordings && chown 65534:65534 /recordings

EXPOSE 8080

USER 65534:65534

ENTRYPOINT ["/server"]
