FROM golang:1.20 AS build-server

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

RUN wget -O /usr/local/bin/dumb-init https://github.com/Yelp/dumb-init/releases/download/v1.2.5/dumb-init_1.2.5_x86_64
RUN chmod +x /usr/local/bin/dumb-init

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot
WORKDIR /app

COPY --from=downloader /usr/local/bin/dumb-init /app/dumb-init
COPY --from=build-server /workspace/server/well-known /app/well-known
USER 65532:65532

ENTRYPOINT ["/app/dumb-init", "--", "/app/well-known"]
