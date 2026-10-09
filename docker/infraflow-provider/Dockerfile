
# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# SQLite utilise github.com/mattn/go-sqlite3 : CGO est nécessaire.
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/infraflow-provider \
    ./provider/cmd

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Indian/Antananarivo \
    INFRAFLOW_PROVIDER_CONFIG=/etc/infraflow/provider-config.yaml \
    INFRAFLOW_PROVIDER_DATA=/var/lib/infraflow-provider

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    openssl \
    sqlite3 \
    curl \
    wget \
    iproute2 \
    iputils-ping \
    traceroute \
    dnsutils \
    net-tools \
    tcpdump \
    netcat-openbsd \
    procps \
    tini \
    && rm -rf /var/lib/apt/lists/*

RUN install -d -m 0755 /etc/infraflow \
    && install -d -m 0750 /var/lib/infraflow-provider

COPY --from=builder /out/infraflow-provider /usr/local/bin/infraflow-provider
COPY examples/provider-config.yaml /etc/infraflow/provider-config.yaml
COPY docker/infraflow-provider/entrypoint.sh /usr/local/bin/infraflow-entrypoint

RUN chmod 0755 /usr/local/bin/infraflow-entrypoint \
    && chmod 0644 /etc/infraflow/provider-config.yaml

VOLUME ["/var/lib/infraflow-provider"]

# 8443 : gRPC ; 8080 : API HTTP et dashboard si activés.
EXPOSE 8443 8080

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/infraflow-entrypoint"]