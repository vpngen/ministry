FROM debian:bookworm-slim

ARG GOLANG_VER=1.24.1
ARG NFPM_VER=2.43.1
ARG TARGETARCH=amd64

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GOLANG_VER}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz

ENV GOPATH=/go
ENV PATH="/usr/local/go/bin:${GOPATH}/bin:${PATH}"

RUN go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@v${NFPM_VER}"

WORKDIR /data
