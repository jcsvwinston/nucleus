FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /app/server examples/mvc_api/main.go || (apk add --no-cache gcc musl-dev && CGO_ENABLED=1 go build -o /app/server examples/mvc_api/main.go)

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server /app/server
COPY --from=builder /src/examples/mvc_api/templates /app/templates

ENV NUCLEUS_EXAMPLE_PORT=8090

EXPOSE 8090
CMD ["/app/server"]
