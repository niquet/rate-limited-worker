# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.24-alpine AS builder

# Accept build arguments for security context
ARG UID=10001
ARG GID=10001

# Install build dependencies and CA certificates
RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    mailcap \
    && update-ca-certificates

# Create non-root user for build stage
RUN addgroup -g ${GID} -S appgroup && \
    adduser -u ${UID} -S appuser -G appgroup

# Set working directory
WORKDIR /build

# Copy dependency files first for better layer caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && \
    go mod verify

# Copy source code
COPY . .

# Set build environment for security and optimization
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    CGO_CPPFLAGS="-D_FORTIFY_SOURCE=2 -fstack-protector-all" \
    GOFLAGS="-buildmode=pie"

# Build the application with security and optimization flags
RUN go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION:-dev} -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) -extldflags '-static'" \
    -a \
    -installsuffix cgo \
    -o worker \
    ./cmd/worker

# Run tests during build to ensure quality
RUN go test -v ./...

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# Accept build arguments
ARG UID=10001
ARG GID=10001

# Labels for better container management
LABEL maintainer="your-team@company.com" \
      version="1.0.0" \
      description="Worker service with OpenTelemetry observability" \
      org.opencontainers.image.source="https://github.com/your-org/worker" \
      org.opencontainers.image.documentation="https://github.com/your-org/worker/README.md" \
      org.opencontainers.image.licenses="MIT"

# Copy timezone data and CA certificates from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/mime.types /etc/mime.types

# Copy the binary from builder stage
COPY --from=builder /build/worker /usr/local/bin/worker

# Copy static web assets if they exist
# COPY --from=builder /build/web/ /app/web/

# Set user to non-root (distroless already provides nonroot user with UID 65532)
# If you need a specific UID, you would need to build a custom base image
USER 65532:65532

# Expose the application port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/worker", "healthcheck"]

# Set the entrypoint
ENTRYPOINT ["/usr/local/bin/worker"]
