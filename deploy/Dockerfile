FROM golang:1.25.1 AS builder
WORKDIR /app
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server ./node/

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/server /server
COPY --from=builder /app/gameconfig /gameconfig
EXPOSE 10090
ENTRYPOINT ["/server", "--config", "/config/k8s.toml"]
