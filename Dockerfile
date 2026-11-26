# Multi-stage build for NKB Diskless System Management

# Stage 1: Build Frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm install
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go Backend
FROM golang:alpine AS backend-builder
WORKDIR /app
RUN apk add --no-cache git
ENV GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf scratch
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/nkb-server ./cmd/server

# Stage 3: Minimal Production Image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata sqlite qemu-img
WORKDIR /app

COPY --from=backend-builder /app/bin/nkb-server /app/nkb-server
COPY --from=frontend-builder /app/frontend/dist /app/frontend/dist
COPY tftpboot /app/tftpboot

# Create data directory mount point
RUN mkdir -p /var/nkb-diskless

EXPOSE 8443

ENTRYPOINT ["/app/nkb-server"]
CMD ["-data", "/var/nkb-diskless", "-port", "8443"]
