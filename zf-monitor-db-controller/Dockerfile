FROM golang:1.22 AS builder

WORKDIR /app

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./

RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o zf-monitor-db-controller .


FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/zf-monitor-db-controller .

CMD ["./zf-monitor-db-controller"]
