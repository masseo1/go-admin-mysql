FROM golang:1.21-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mysql-admin .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -g 1001 app && adduser -D -u 1001 -G app app
WORKDIR /app
COPY --from=builder /app/mysql-admin .
RUN mkdir -p /data && chown app:app /data
USER app
EXPOSE 8080
ENTRYPOINT ["./mysql-admin"]
