# syntax=docker/dockerfile:1

# ---- build ----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27.2 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT" \
    -o /out/payback-coupons ./cmd/payback-coupons

# ---- runtime --------------------------------------------------------------
# Debian ships Chromium for amd64 and arm64.
FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends chromium ca-certificates fonts-liberation \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10001 --no-create-home --shell /usr/sbin/nologin app \
 && mkdir /data && chown app:app /data

COPY --from=build /out/payback-coupons /usr/local/bin/payback-coupons

# The Chromium sandbox needs privileges a locked-down container does not
# have; the container itself is the isolation boundary and the browser
# only ever visits payback.de.
ENV CHROME_PATH=/usr/bin/chromium \
    CHROME_NO_SANDBOX=true \
    COOKIES_FILE=/data/cookies.json \
    TZ=Europe/Berlin \
    HOME=/tmp

USER app
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/payback-coupons"]
