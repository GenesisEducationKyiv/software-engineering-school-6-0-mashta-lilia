FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /server ./main

FROM alpine:3.19

RUN apk --no-cache add ca-certificates wget \
    && addgroup -S app && adduser -S app -G app
WORKDIR /app

COPY --from=builder --chown=app:app /server .
COPY --chown=app:app migrations/ ./migrations/
COPY --chown=app:app swagger.yaml ./

USER app
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health || exit 1

CMD ["./server"]
