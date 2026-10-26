FROM node:26-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27.3-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY pluginapi/go.mod pluginapi/go.sum ./pluginapi/
RUN go mod download
COPY . .
COPY --from=frontend /cmd/webui/dist ./cmd/webui/dist
RUN CGO_ENABLED=0 go build -o /webui ./cmd/webui

FROM alpine:3.24.3
RUN apk add --no-cache ca-certificates
COPY --from=builder /webui /usr/local/bin/webui
EXPOSE 8080
CMD ["/usr/local/bin/webui"]
