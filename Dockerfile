# ============================
# Stage 1: Build
# ============================
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files terlebih dahulu (optimasi cache layer)
COPY go.mod go.sum ./
RUN go mod download

# Copy seluruh source code
COPY . .

# Build binary
# -ldflags="-s -w" untuk mengurangi ukuran binary (strip debug info)
# CGO_ENABLED=0 untuk static linking
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w" \
    -o /app/server \
    ./cmd/server

# ============================
# Stage 2: Runtime
# ============================
FROM alpine:3.19

# Install ca-certificates untuk HTTPS (Resend API, Google OAuth)
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user untuk keamanan
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Set working directory
WORKDIR /app

# Copy binary dari stage builder
COPY --from=builder /app/server .

# Copy migrations (jika dibutuhkan untuk auto-migrasi manual)
COPY --from=builder /app/internal/db/migrations ./internal/db/migrations

# Ganti kepemilikan ke non-root user
RUN chown -R appuser:appgroup /app

# Switch ke non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Run binary
CMD ["./server"]
