
FROM golang:1.25.4 AS builder

WORKDIR /app

# 1) copy go.mod/go.sum จาก root context
COPY go.mod go.sum ./
RUN go mod download

# 2) copy โค้ดทั้งหมด (etolib + examples + otelgo.go ฯลฯ)
COPY . .

# 3) build binary จากโฟลเดอร์ examples/http-basic
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o http-basic ./examples/http-basic

# =========================
# Runtime stage
# =========================
FROM alpine:3.20

WORKDIR /app
COPY --from=builder /app/http-basic .

EXPOSE 8090

ENV SERVICE_NAME=http-basic
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317

CMD ["./http-basic"]