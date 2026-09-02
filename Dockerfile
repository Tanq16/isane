FROM --platform=$BUILDPLATFORM golang:1.27.0-trixie AS builder

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
      curl jq make openssl tar \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH VERSION=dev-build

RUN make assets && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X 'github.com/tanq16/isane/cmd.AppVersion=${VERSION}'" \
      -o /app/isane .

FROM debian:trixie-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates tzdata ffmpeg imagemagick libmagickcore-7.q16-10-extra \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd -g 10001 isane \
    && useradd -u 10001 -g isane -M -s /usr/sbin/nologin isane

WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/isane .

RUN mkdir -p /media && chown 10001:10001 /media
VOLUME ["/media"]

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["./isane"]
CMD ["serve", "--config", "/config.yaml"]
