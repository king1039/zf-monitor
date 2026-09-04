# =========================
# Build Stage
# =========================
FROM golang:1.22 AS builder

WORKDIR /app

# 使用国内 Go Module Proxy
ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o zf-monitor-back .


# =========================
# Runtime Stage
# =========================
FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/zf-monitor-back .

COPY web ./web

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./zf-monitor-back"]
