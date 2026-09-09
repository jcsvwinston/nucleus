# Container image for showcase_demo.
#
# Every database driver Nucleus publishes is pure Go (modernc SQLite, pgx,
# go-sql-driver/mysql, go-mssqldb, go-ora), so this binary needs no cgo and
# no C library at run time. That is what lets the runtime stage hold the
# binary, the configuration and nothing else.
#
#   docker build -t showcase_demo .
#   docker run --rm -p 8091:8091 showcase_demo
#
# `nucleus doctor --check image` reads this file and reports the
# misconfigurations that are easy to introduce while editing it.

# --- build -------------------------------------------------------------------
# The toolchain tag matches the `go` directive in go.mod, so the image
# compiles this module with the version it declares.
FROM golang:1.26.6-alpine AS build

WORKDIR /src

# Dependencies first: this layer is reused by every build that leaves go.mod
# and go.sum untouched. go.sum is not optional — it carries the checksum of
# every module the build downloads, and `go mod download` verifies against
# it. A project scaffolded with --offline has no go.sum yet; run
# `go mod tidy` before the first build.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 links no libc, so the binary runs on a base that has none.
# -trimpath keeps the build machine's directory names out of the binary.
# -s -w drop the symbol table and DWARF, which nothing at run time reads.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app .

# A writable directory for the runtime image. The runtime base has no shell,
# so there is no RUN there to create one: it is made here and copied over
# with its ownership. Docker initialises an empty volume from the image, so
# a named volume mounted on /data inherits that ownership too — which is
# what lets the container write anything at all without running as root.
RUN mkdir -p /data

# --- runtime -----------------------------------------------------------------
# distroless/static carries CA certificates, timezone data and /etc/passwd,
# and no shell, package manager or libc: there is nothing here for an
# injected command to run, and no distribution packages to patch.
#
# The digest is the pin that counts. A tag can be moved to point at
# different bytes; a digest cannot, so this line is the exact image that was
# reviewed. Change it deliberately, not on a rebuild.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

WORKDIR /app

COPY --from=build /out/app /app/app
COPY --from=build --chown=65532:65532 /data /data

# nucleus.yml travels in the image as the default. Anything that differs per
# environment — database URL, secrets — arrives as NUCLEUS_* variables at run
# time, so it is never baked into a layer anyone who pulls the image can read.
COPY nucleus.yml /app/nucleus.yml
# The Casbin enforcer is default-deny and nucleus.yml names this file, so the
# app refuses to start without it.
COPY rbac_policy.csv /app/rbac_policy.csv

# nucleus.yml starts this project on SQLite, which writes app.db in the
# working directory — and /app belongs to root. /data is the writable path:
#
#   docker run -v showcase_demo-data:/data \
#       -e NUCLEUS_DATABASES__DEFAULT__URL=sqlite:///data/app.db \
#       -p 8091:8091 showcase_demo
#
# A server engine (`--db postgres` at scaffold time, or the same variable
# pointed at one) needs none of this.

# 65532 is the base image's `nonroot` user, written numerically so an
# orchestrator enforcing runAsNonRoot can read it without resolving a name.
# Nothing under /app belongs to that user, which is the point: the process
# cannot rewrite its own binary or configuration.
USER 65532:65532

EXPOSE 8091

# No HEALTHCHECK: this base has no shell and no HTTP client to run one.
# Point your orchestrator's probe at GET /healthz on port 8091 instead.
ENTRYPOINT ["/app/app"]
