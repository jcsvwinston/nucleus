# Builds the `nucleus` CLI image. Every driver the CLI links is pure Go
# (modernc SQLite, pgx, go-sql-driver/mysql, go-mssqldb, go-ora), so the
# binary is static: no cgo, no C toolchain in the builder, nothing but the
# binary and CA certificates in the runtime image.
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nucleus ./cmd/nucleus

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/nucleus /app/nucleus
USER 65532:65532

ENTRYPOINT ["/app/nucleus"]
CMD ["--help"]
