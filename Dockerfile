FROM --platform=$BUILDPLATFORM golang:1.27.0-alpine AS builder

WORKDIR /app

RUN apk add --no-cache curl jq make openssl tar

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS TARGETARCH VERSION=dev-build

RUN make assets && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
      -ldflags="-s -w -X 'main.AppVersion=${VERSION}'" \
      -o /app/isane ./cmd/isane

FROM alpine:3.24.1

RUN apk add --no-cache \
      ca-certificates tzdata ffmpeg \
      imagemagick imagemagick-heic imagemagick-jpeg imagemagick-tiff imagemagick-webp && \
    addgroup -g 10001 -S isane && \
    adduser -u 10001 -S -G isane isane

WORKDIR /app
COPY --from=builder --chown=10001:10001 /app/isane .

RUN mkdir -p /media && chown 10001:10001 /media
VOLUME ["/media"]

USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["./isane"]
CMD ["--config", "/config.yaml"]
