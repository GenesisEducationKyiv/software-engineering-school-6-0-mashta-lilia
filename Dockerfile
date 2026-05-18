FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./main

FROM alpine:3.19

RUN apk --no-cache add ca-certificates \
    && addgroup -S app && adduser -S app -G app
WORKDIR /app

COPY --from=builder --chown=app:app /server .
COPY --chown=app:app migrations/ ./migrations/
COPY --chown=app:app swagger.yaml ./

USER app
EXPOSE 8080

CMD ["./server"]
