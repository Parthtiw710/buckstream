# --- Build Stage ---
FROM golang:1.25-alpine AS builder

# Install SSL certificates so we can copy them to scratch
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy dependency files and download
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ ./cmd/
COPY pkg/ ./pkg/

# Build statically linked binary and strip debugging symbols
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /buckstream cmd/main.go

# --- Run Stage ---
FROM scratch

# Copy SSL certificates from builder stage to allow HTTPS cloud API requests
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder stage
COPY --from=builder /buckstream .

# Expose broker port
EXPOSE 8080

# Run broker
ENTRYPOINT ["./buckstream"]
