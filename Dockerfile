# Build stage: Node.js for frontend
FROM node:20-alpine AS node-builder

WORKDIR /app

# Copy web directory
COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ ./
RUN npm run build

# Build stage: Go for backend
FROM golang:1.24-alpine AS go-builder

WORKDIR /app

# Copy Go module files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend from node-builder
COPY --from=node-builder /app/internal/webui/dist ./internal/webui/dist

# Build the Go binary
RUN CGO_ENABLED=0 GOOS=linux go build -tags embedui -trimpath -ldflags "-s -w -X main.version=${VERSION:-dev}" -o /financeanalysis ./cmd/financeanalysis

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy the binary
COPY --from=go-builder /financeanalysis /financeanalysis

# Set environment variables
ENV FA_DATA_DIR=/data
ENV FA_ADDR=0.0.0.0:8080
ENV FA_LOG_LEVEL=info

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/financeanalysis", "healthcheck"]

# Run the application
ENTRYPOINT ["/financeanalysis", "serve"]
