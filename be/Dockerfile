# One image, any daemon: build with --build-arg CMD=backupd (or apid, main-bot,
# init-bot, deliveryd, catalog-bot). Every cmd/<name> package in this module builds the same way -- composition
# only, no daemon-specific build logic (R-GO-3) -- so one Dockerfile serves all of them instead of
# five nearly-identical ones drifting apart over time.

FROM golang:1.25-alpine AS build
WORKDIR /src

# Deps first, so an source-only change doesn't invalidate this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG CMD
RUN test -n "$CMD" || (echo "docker build --build-arg CMD=<backupd|apid|...> is required" >&2 && exit 1)
# CGO_ENABLED=0: nothing here uses cgo (pgx/v5 is pure Go), so the binary is static and needs
# nothing from the build image at runtime -- which is what makes the distroless stage below possible.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/daemon "./cmd/${CMD}"

# distroless: no shell, no package manager, nothing in the image to exploit beyond the binary
# itself. static-debian12 still carries CA certificates, needed for every outbound HTTPS call these
# daemons make (Telegram Bot API, TMDB). :nonroot runs as an unprivileged user by default.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/daemon /daemon
ENTRYPOINT ["/daemon"]
