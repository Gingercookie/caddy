FROM golang:1.23 AS builder

WORKDIR /app

COPY . .
RUN go mod download
RUN GOARCH=arm64 GOOS=linux go build -o dns-caddy .

# FROM scratch
# COPY --from=builder /app/dns-caddy /dns-caddy

# Command to run the application
ENTRYPOINT ["/app/dns-caddy"]
