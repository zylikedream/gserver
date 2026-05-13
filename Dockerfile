FROM golang:1.25.1-alpine AS builder
ENV GOPROXY=https://goproxy.cn
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o game-server node/main.go

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /build/game-server .
COPY --from=builder /build/gameconfig ./gameconfig
ENTRYPOINT ["./game-server"]
