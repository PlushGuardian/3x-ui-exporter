ARG MAIN_PACKAGE_PATH=./cmd/x-ui-exporter
ARG BINARY_NAME=x-ui-exporter
ARG IMAGE_SOURCE=https://github.com/PlusGuardian/3x-ui-exporter

FROM golang:1.24 AS builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG GIT_TAG
ARG GIT_COMMIT

ARG MAIN_PACKAGE_PATH
ARG BINARY_NAME
ARG IMAGE_SOURCE

ENV CGO_ENABLED=0
ENV GO111MODULE=on
ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}

WORKDIR /build

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-w -s -X main.version=${GIT_TAG} -X main.commit=${GIT_COMMIT}" \
    -trimpath \
    -o /usr/bin/${BINARY_NAME} ${MAIN_PACKAGE_PATH} && \
    go clean -modcache

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.source=${IMAGE_SOURCE}

ARG BINARY_NAME

USER nonroot:nonroot

WORKDIR /
COPY --from=builder --chown=nonroot:nonroot /usr/bin/${BINARY_NAME} /${BINARY_NAME}

ENV PATH="/:${PATH}"

ENTRYPOINT ["/${BINARY_NAME}"]
