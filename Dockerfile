FROM oven/bun:1.4.0@sha256:5ff609364c049b54eb0ff560ec96319729a972078ef2c755d758f0c6ef89c2d6 AS builder

WORKDIR /build
COPY web/package.json .
COPY web/bun.lock .
RUN --mount=type=cache,target=/root/.bun/install/cache bun install --frozen-lockfile
COPY ./web .
COPY ./VERSION .
ARG BUILD_VERSION=v0.0.0
RUN VERSION_FROM_FILE="$(tr -d '\r\n' < VERSION)" \
    && RESOLVED_VERSION="${VERSION_FROM_FILE:-${BUILD_VERSION:-v0.0.0}}" \
    && DISABLE_ESLINT_PLUGIN='true' VITE_REACT_APP_VERSION="${RESOLVED_VERSION}" bun run build

FROM golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba AS builder2
ENV GO111MODULE=on CGO_ENABLED=0
# eflow: 国内构建走 goproxy.cn,避免默认 proxy.golang.org 拉依赖 EOF/超时;
# ,direct 回退保证国外网络也能用。
ENV GOPROXY=https://goproxy.cn,direct

ARG TARGETOS
ARG TARGETARCH
ENV GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64}
ENV GOEXPERIMENT=greenteagc

WORKDIR /build

ADD go.mod go.sum ./
COPY relaykit/go.mod relaykit/go.sum ./relaykit/
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
COPY --from=builder /build/dist ./web/dist
ARG BUILD_VERSION=v0.0.0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    VERSION_FROM_FILE="$(tr -d '\r\n' < VERSION)" \
    && RESOLVED_VERSION="${VERSION_FROM_FILE:-${BUILD_VERSION:-v0.0.0}}" \
    && go build -ldflags "-s -w -X 'eflow.local/new-api/common.Version=${RESOLVED_VERSION}'" -o new-api

FROM debian:bookworm-slim@sha256:f06537653ac770703bc45b4b113475bd402f451e85223f0f2837acbf89ab020a

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata libasan8 wget \
    && rm -rf /var/lib/apt/lists/* \
    && update-ca-certificates

COPY --from=builder2 /build/new-api /
ARG APPLICATION_FINGERPRINT
LABEL cc.e-flowcode.application-fingerprint=$APPLICATION_FINGERPRINT
EXPOSE 3000
WORKDIR /data
ENTRYPOINT ["/new-api"]
