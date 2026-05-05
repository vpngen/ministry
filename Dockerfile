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

RUN mkdir -p bin \
    && go build -o bin/checkbrigadier   ./cmd/checkbrigadier \
    && go build -o bin/ckvip            ./cmd/ckvip \
    && go build -o bin/reqvipid         ./cmd/reqvipid \
    && go build -o bin/readmsgs         ./cmd/readmsgs \
    && go build -o bin/createbrigade    ./cmd/createbrigade \
    && go build -o bin/restorebrigadier ./cmd/restorebrigadier \
    && go build -o bin/syncstats        ./cmd/syncstats \
    && go build -o bin/recodesnaps      ./cmd/recodesnaps \
    && go build -o bin/recodesnapmap    ./cmd/recodesnapmap \
    && go build -o bin/synclabels       ./cmd/synclabels

# ── runtime ────────────────────────────────────────────────────────────────────
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        openssh-client \
        sudo \
        postgresql-client \
        jq \
    && rm -rf /var/lib/apt/lists/*

RUN useradd -r -m -s /bin/bash vg_head_admin \
    && useradd -r -m -s /bin/bash vg_head_vpnapi \
    && useradd -r -m -s /bin/bash vg_partners_admin \
    && useradd -r -m -s /bin/bash vg_head_stats \
    && useradd -r -m -s /bin/bash vg_head_migr

RUN mkdir -p \
        /opt/vg-head-vpnapi \
        /opt/vg-head-stats \
        /opt/vg-head-admin \
        /opt/vg-partners-admin \
        /etc/vgdept \
        /usr/share/vg-head

# binaries
COPY --from=builder /src/bin/checkbrigadier    /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/ckvip             /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/reqvipid          /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/readmsgs          /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/createbrigade     /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/restorebrigadier  /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/recodesnaps       /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/recodesnapmap     /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/synclabels        /opt/vg-head-vpnapi/
COPY --from=builder /src/bin/syncstats         /opt/vg-head-stats/

# shell scripts
COPY scripts/                               /opt/vg-head-vpnapi/
COPY cmd/sshcmd/ssh_command.sh              /opt/vg-head-vpnapi/
COPY cmd/switchremotemigr/switch_remote_migr.sh /opt/vg-head-vpnapi/
COPY cmd/realms/realms.sh                   /opt/vg-head-admin/
COPY cmd/partners/partners.sh               /opt/vg-partners-admin/
COPY cmd/partners/tokens.sh                 /opt/vg-partners-admin/

# sql and config
COPY sql/                                   /usr/share/vg-head/sql/
COPY debpkg/src/vg-head-stats.env.sample    /etc/vgdept/

WORKDIR /home/vg_head_vpnapi
CMD ["/bin/bash"]
