FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN CGO_ENABLED=0 go build -o /reviewer ./cmd/reviewer

FROM alpine:3.20
RUN apk add --no-cache git ca-certificates

COPY --from=builder /reviewer /usr/local/bin/reviewer

CMD ["/usr/local/bin/reviewer"]
