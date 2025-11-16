FROM golang:1.25.4 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o http-gin ./examples/http-gin

FROM alpine:3.20

WORKDIR /app
COPY --from=builder /app/http-gin .

EXPOSE 8091

ENV SERVICE_NAME=http-gin
ENV OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317

CMD ["./http-gin"]