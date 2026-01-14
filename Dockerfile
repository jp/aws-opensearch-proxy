# Build stage
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version information
ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

# Build the binary with version information
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.GitCommit=${VCS_REF}" \
    -o aws-opensearch-proxy .

# Final stage
FROM alpine:3.19

# Build metadata
ARG VERSION=dev
ARG BUILD_DATE
ARG VCS_REF

# Add metadata labels
LABEL org.opencontainers.image.title="AWS OpenSearch Proxy" \
      org.opencontainers.image.description="HTTP proxy for Amazon OpenSearch with automatic AWS SigV4 request signing" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.source="https://github.com/jpellet/aws-opensearch-proxy" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="julien" \
      maintainer="jpellet"

# Install ca-certificates for HTTPS and wget for health checks
RUN apk --no-cache add ca-certificates wget

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/aws-opensearch-proxy .

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser && \
    chown -R appuser:appuser /root

USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/_health || exit 1

# Run the binary
ENTRYPOINT ["./aws-opensearch-proxy"]
