FROM golang:1.24.1 AS builder

WORKDIR /app

COPY go.mod go.sum /app/
RUN go mod download

COPY cmd /app/cmd
COPY internal /app/internal
COPY .env.example /app/.env.example

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/diatune-safe ./cmd/diatune-safe

FROM gcr.io/distroless/static-debian12

WORKDIR /app
COPY --from=builder /out/diatune-safe /app/diatune-safe
COPY --from=builder /app/.env.example /app/.env.example

EXPOSE 8080

ENTRYPOINT ["/app/diatune-safe"]
CMD ["api", "--host", "0.0.0.0", "--port", "8080"]
