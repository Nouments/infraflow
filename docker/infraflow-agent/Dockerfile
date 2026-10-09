
# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/infraflow-agent \
    ./agent/cmd

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Indian/Antananarivo \
    INFRAFLOW_AGENT_CONFIG=/etc/infraflow/agent-config.yaml \
    INFRAFLOW_AGENT_DATA=/var/lib/infraflow-agent

# Outils système et diagnostic réseau
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    openssh-client \
    openssh-server \
    iproute2 \
    iputils-ping \
    traceroute \
    curl \
    wget \
    dnsutils \
    net-tools \
    tcpdump \
    netcat-openbsd \
    mtr-tiny \
    procps \
    git \
    unzip \
    python3 \
    python3-pip \
    python3-venv \
    ansible \
    tini \
    gettext-base \
    && rm -rf /var/lib/apt/lists/*

# Terraform depuis le dépôt officiel HashiCorp
RUN apt-get update \
    && apt-get install -y --no-install-recommends gnupg \
    && install -d -m 0755 /usr/share/keyrings \
    && curl -fsSL https://apt.releases.hashicorp.com/gpg \
       | gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg \
    && chmod 0644 /usr/share/keyrings/hashicorp-archive-keyring.gpg \
    && echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com bookworm main" \
       > /etc/apt/sources.list.d/hashicorp.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends terraform \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --shell /bin/bash infra \
    && install -d -m 0755 /run/sshd \
    && install -d -m 0755 /etc/infraflow \
    && install -d -o infra -g infra -m 0750 /var/lib/infraflow-agent \
    && install -d -o infra -g infra -m 0750 /home/infra/.ssh \
    && chmod 0700 /home/infra/.ssh \
    && ssh-keygen -A

COPY --from=builder /out/infraflow-agent /usr/local/bin/infraflow-agent
COPY examples/agent-config.yaml /etc/infraflow/agent-config.yaml
COPY docker/infraflow-agent/entrypoint.sh /usr/local/bin/infraflow-entrypoint

RUN chmod 0755 /usr/local/bin/infraflow-entrypoint \
    && chmod 0644 /etc/infraflow/agent-config.yaml

VOLUME ["/var/lib/infraflow-agent", "/home/infra/.ssh"]

EXPOSE 22

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/infraflow-entrypoint"]