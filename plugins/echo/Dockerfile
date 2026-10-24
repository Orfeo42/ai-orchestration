FROM golang:1.27.3-alpine AS builder
WORKDIR /build
COPY pluginapi/go.mod pluginapi/go.sum ./pluginapi/
COPY plugins/echo/go.mod plugins/echo/go.sum ./plugins/echo/
WORKDIR /build/plugins/echo
ENV GOWORK=off
RUN go mod download
WORKDIR /build
COPY pluginapi ./pluginapi
COPY plugins/echo ./plugins/echo
WORKDIR /build/plugins/echo
RUN CGO_ENABLED=0 go build -o /echo .

FROM alpine:3.24.2
RUN apk add --no-cache ca-certificates
COPY --from=builder /echo /usr/local/bin/echo-plugin
EXPOSE 8090
CMD ["/usr/local/bin/echo-plugin"]
