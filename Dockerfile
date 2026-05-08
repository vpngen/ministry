ARG GOLANG_VER=1.24.1

# ── builder ────────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim AS builder

ARG GOLANG_VER
ARG GIT_USER=git
ARG GIT_PASSWORD

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GOLANG_VER}.linux-amd64.tar.gz" \
    | tar -C /usr/local -xz

ENV GOPATH=/go
ENV PATH="/usr/local/go/bin:${GOPATH}/bin:${PATH}"
ENV CGO_ENABLED=0

RUN git config --global credential.helper \
        "!f() { echo \"username=${GIT_USER}\"; echo \"password=${GIT_PASSWORD}\"; }; f" \
    && go env -w GOPRIVATE=github.com/vpngen

WORKDIR /src
COPY . .

RUN go build -o bin/ministry-api ./cmd/ministry-api

# ── runtime ────────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/ministry-api /usr/local/bin/ministry-api

CMD ["/usr/local/bin/ministry-api"]
