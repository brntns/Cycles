# syntax=docker/dockerfile:1
FROM golang:1-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM alpine:3
RUN apk add --no-cache ca-certificates \
    && adduser -D -u 10001 app
USER app
WORKDIR /app
COPY --from=builder /out/server /app/server

ENV PORT=4715
EXPOSE 4715
ENTRYPOINT ["/app/server"]
