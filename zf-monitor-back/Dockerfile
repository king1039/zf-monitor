# =========================
# Build Stage
# =========================
FROM golang:1.22 AS builder

WORKDIR /app

# 使用国内 Go Module Proxy
ENV GOPROXY=https://goproxy.cn,direct

COPY zf-monitor-back/go.mod zf-monitor-back/go.sum ./

RUN go mod download

COPY zf-monitor-back .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o zf-monitor-back .


# =========================
# Runtime Stage
# =========================
FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/zf-monitor-back .

COPY zf-monitor-back/web ./web

COPY database ./databse

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./zf-monitor-back"]
