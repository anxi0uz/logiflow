FROM docker.io/library/golang:1.25-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/logiflow ./cmd/main.go

FROM docker.io/library/alpine:3.20

RUN addgroup -g 1000 app && adduser -u 1000 -G app -D -H -s /sbin/nologin app

COPY --from=builder /bin/logiflow /app/logiflow
COPY --chown=app:app configs/ /app/configs/
COPY --chown=app:app migrations/ /app/migrations/

USER app

WORKDIR /app

EXPOSE 3001

CMD ["/app/logiflow"]
