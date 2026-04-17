FROM golang:1.26-alpine AS builder
WORKDIR /build
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN CGO_ENABLED=0 go build -o /producer ./cmd/producer

FROM alpine:3.20
RUN apk add --no-cache ca-certificates

COPY --from=builder /producer /usr/local/bin/producer

CMD ["/usr/local/bin/producer"]
