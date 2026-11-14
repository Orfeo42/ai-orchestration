FROM golang:1.27.4-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY pluginapi/go.mod pluginapi/go.sum ./pluginapi/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /migrate ./cmd/migrate

FROM alpine:3.24.4
RUN apk add --no-cache ca-certificates

COPY --from=builder /migrate /usr/local/bin/migrate

CMD ["/usr/local/bin/migrate"]
