# Builds the `nucleus` CLI image. Every driver the CLI links is pure Go
# (modernc SQLite, pgx, go-sql-driver/mysql, go-mssqldb, go-ora), so the
# binary is static: no cgo, no C toolchain in the builder, and nothing in the
# runtime image has to supply a libc for it.
#
# Two targets share one runtime shape:
#
#   docker build .                -> `local`, the default target. Compiles the
#                                   CLI from this checkout. This is the one to
#                                   use on a workstation.
#   docker build --target release -> `release`. No compiler at all: it copies a
#                                   binary GoReleaser already produced for the
#                                   tag, so the published image carries the
#                                   same bytes the release's signed
#                                   checksums.txt covers. Its build context is
#                                   NOT this repository — it is a directory
#                                   holding only <os>/<arch>/nucleus, laid out
#                                   by .github/workflows/release.yml, which is
#                                   the only caller. Nothing else from the
#                                   checkout can reach that image, and a COPY
#                                   added there expecting repository files will
#                                   not find them.
#
# The runtime is distroless static rather than alpine: no shell, no package
# manager, no apk database. Its own documentation (base/README.md in
# GoogleContainerTools/distroless) lists what the `static` image contains —
# ca-certificates, a /etc/passwd entry, /tmp and tzdata — which is what the
# `apk add --no-cache ca-certificates tzdata` line it replaces was there for.
# The base is pinned by digest because `nonroot` is a moving tag, and 65532 is
# the uid that tag's user has (NONROOT in the distroless common/variables.bzl).
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nucleus ./cmd/nucleus

# The image the release publishes. TARGETOS/TARGETARCH are filled in by
# buildx for each platform of the manifest list; the stage runs no command, so
# building linux/arm64 on an amd64 runner needs no emulation.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS release
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app
COPY ${TARGETOS}/${TARGETARCH}/nucleus /app/nucleus
USER 65532:65532

ENTRYPOINT ["/app/nucleus"]
CMD ["--help"]

# The default target: same runtime, binary compiled above from this checkout.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS local
WORKDIR /app
COPY --from=builder /out/nucleus /app/nucleus
USER 65532:65532

ENTRYPOINT ["/app/nucleus"]
CMD ["--help"]
