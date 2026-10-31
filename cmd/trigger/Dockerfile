FROM golang:1.27.3-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY pluginapi/go.mod pluginapi/go.sum ./pluginapi/
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /trigger ./cmd/trigger

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /trigger /usr/local/bin/trigger
COPY workflows /workflows

CMD ["/usr/local/bin/trigger"]
