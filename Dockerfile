# syntax=docker/dockerfile:1.7

FROM --platform=$TARGETPLATFORM alpine AS node-assets
ARG TARGETARCH
ARG TARGETVARIANT
RUN apk add --no-cache nodejs zstd && \
    case "$TARGETARCH/$TARGETVARIANT" in \
        "amd64/") zstd -f /usr/bin/node -o /tmp/node.zst ;; \
        "arm64/") zstd -f /usr/bin/node -o /tmp/node.zst ;; \
        "arm/v7") zstd -f /usr/bin/node -o /tmp/node.zst ;; \
        *) echo "不支持的架构: $TARGETARCH/$TARGETVARIANT" && exit 1 ;; \
    esac

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /app
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download
COPY . .
COPY --from=node-assets /tmp/node.zst /tmp/node.zst
ARG GITHUB_SHA
ARG VERSION
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    echo "Building commit: ${GITHUB_SHA:0:7}" && \
    case "$TARGETARCH/$TARGETVARIANT" in \
        "amd64/") cp /tmp/node.zst assets/node_linux_amd64.zst ;; \
        "arm64/") cp /tmp/node.zst assets/node_linux_arm64.zst ;; \
        "arm/v7") cp /tmp/node.zst assets/node_linux_armv7.zst ;; \
        *) echo "不支持的架构: $TARGETARCH/$TARGETVARIANT" && exit 1 ;; \
    esac && \
    export GOOS="$TARGETOS" GOARCH="$TARGETARCH" && \
    if [ "$TARGETARCH" = "arm" ]; then export GOARM="${TARGETVARIANT#v}"; fi && \
    CGO_ENABLED=0 go build -mod=readonly -ldflags="-s -w -X main.Version=${VERSION} -X main.CurrentCommit=${GITHUB_SHA:0:7}" -trimpath -o subs-check

FROM alpine
WORKDIR /app
ENV TZ=Asia/Shanghai
RUN apk add --no-cache alpine-conf ca-certificates nodejs && \
    /usr/sbin/setup-timezone -z Asia/Shanghai && \
    apk del alpine-conf && \
    rm -rf /var/cache/apk/* && \
    rm -rf /usr/bin/node
COPY --from=builder /app/subs-check /app/subs-check
CMD ["/app/subs-check"]
EXPOSE 8199
EXPOSE 8299
