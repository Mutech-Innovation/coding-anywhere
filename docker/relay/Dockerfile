# syntax=docker/dockerfile:1.7

# --- builder ---------------------------------------------------------------
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Cache modules first so source changes don't bust the dep layer.
COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build \
        -ldflags "-s -w \
            -X github.com/Mutech-Innovation/coding-anywhere/internal/version.Version=${VERSION} \
            -X github.com/Mutech-Innovation/coding-anywhere/internal/version.Commit=${COMMIT} \
            -X github.com/Mutech-Innovation/coding-anywhere/internal/version.BuildDate=${BUILD_DATE}" \
        -o /out/coding-anywhere-relay \
        ./apps/relay/cmd

# --- runtime ---------------------------------------------------------------
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tini

COPY --from=builder /out/coding-anywhere-relay /usr/local/bin/coding-anywhere-relay

USER nobody
EXPOSE 8443
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/coding-anywhere-relay"]
CMD ["-addr=:8443"]
