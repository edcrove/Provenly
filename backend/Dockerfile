# syntax=docker/dockerfile:1
# Provenly API. Targets:
#   dev     - Go toolchain + air: rebuilds and restarts on every source change (hot reload).
#   runtime - small alpine image with the static binary (default).
ARG GO_VERSION=1.27.1

FROM golang:${GO_VERSION}-alpine AS base
# Optional extra CA (corporate TLS-intercepting proxy): `--secret id=extra_ca,src=<pem>`.
# Secrets are not part of the build cache key: EXTRA_CA_ID (the CA's hash, set by the Makefile) is, so adding or
# changing the CA re-runs this layer instead of reusing one built without it.
ARG EXTRA_CA_ID=none
RUN --mount=type=secret,id=extra_ca,required=false \
    echo "extra CA: ${EXTRA_CA_ID}" && if [ -s /run/secrets/extra_ca ]; then cat /run/secrets/extra_ca >> /etc/ssl/certs/ca-certificates.crt; fi
WORKDIR /src
COPY go.mod go.sum ./
# Modules stay in the image layer (cached while go.sum is unchanged) so the dev
# target's module volume starts populated.
RUN go mod download

FROM base AS dev
RUN go install github.com/air-verse/air@latest
ENV PROVENLY_HTTP_ADDR=:8080
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

FROM base AS build
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/provenly ./cmd/provenly

FROM alpine:3 AS runtime
RUN addgroup -S provenly && adduser -S -G provenly provenly
COPY --from=build /out/provenly /usr/local/bin/provenly
USER provenly
ENV PROVENLY_HTTP_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=6 \
    CMD wget -qO- http://127.0.0.1:8080/readyz >/dev/null || exit 1
ENTRYPOINT ["provenly"]
CMD ["serve"]
