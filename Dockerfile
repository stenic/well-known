FROM golang:1.23 AS build-server

WORKDIR /workspace/server
# Copy the Go Modules manifests
COPY ./go.* .
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY ./server/*.go ./

# Test
RUN go test -v ./...

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -o well-known ./


FROM alpine AS downloader

ARG TARGETPLATFORM
ARG TINI_VERSION=v0.19.0
RUN if [ "$TARGETPLATFORM" = "linux/amd64" ]; then ARCHITECTURE=amd64; elif [ "$TARGETPLATFORM" = "linux/arm/v7" ]; then ARCHITECTURE=arm; elif [ "$TARGETPLATFORM" = "linux/arm64" ]; then ARCHITECTURE=arm64; else ARCHITECTURE=amd64; fi \
    && wget -O /usr/local/bin/tini https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-static-${ARCHITECTURE}
RUN chmod +x /usr/local/bin/tini

#

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /app

COPY --from=downloader /usr/local/bin/tini /app/tini
COPY --from=build-server /workspace/server/well-known /app/well-known
USER 65532:65532

ENTRYPOINT ["/app/tini", "--", "/app/well-known"]
