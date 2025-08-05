ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.22
FROM --platform=${BUILDPLATFORM} golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS build

WORKDIR /app

RUN apk -U add nodejs npm

COPY go.mod go.sum ./
RUN \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/root/go \
  go mod download

COPY package.json package-lock.json ./
RUN \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/root/go \
  --mount=type=cache,target=/app/node_modules \
  npm ci

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/root/go \
  --mount=type=cache,target=/app/node_modules \
  npm run generate

RUN \
  --mount=type=cache,target=/root/.cache \
  --mount=type=cache,target=/root/go \
  --mount=type=cache,target=/app/node_modules \
  GOOS=${TARGETOS} \
  GOARCH=${TARGETARCH} \
  CGO_ENABLED=0 \
  go build \
  -gcflags "all=-N -l" \
  -o /app/bin/web \
  ./cmd/web

FROM alpine:${ALPINE_VERSION} AS run
WORKDIR /app

RUN apk -U add ca-certificates mailcap \
  && mkdir -p /app/var

COPY --from=build /app/bin/web /app/bin/web

CMD ["/app/bin/web"]

LABEL org.opencontainers.image.source="https://github.com/tigrisdata-community/tygen"
