#!/usr/bin/env bash
set -Eeuo pipefail

CONFIG="${INFRAFLOW_AGENT_CONFIG:-/etc/infraflow/agent-config.yaml}"
DATA="${INFRAFLOW_AGENT_DATA:-/var/lib/infraflow-agent}"

mkdir -p "$DATA" /run/sshd /home/infra/.ssh
chmod 0700 /home/infra/.ssh
chown -R infra:infra "$DATA" /home/infra/.ssh

# Mot de passe de laboratoire seulement.
# En production, préférer les clés SSH et désactiver l'authentification
# par mot de passe.
if [[ "${ENABLE_SSHD:-true}" == "true" ]]; then
    if [[ -n "${INFRA_SSH_PASSWORD:-}" ]]; then
        echo "infra:${INFRA_SSH_PASSWORD}" | chpasswd
    elif [[ "${ALLOW_DEFAULT_LAB_PASSWORD:-false}" == "true" ]]; then
        echo 'infra:infra' | chpasswd
    else
        passwd -l infra >/dev/null 2>&1 || true
    fi

    if [[ -n "${INFRA_SSH_AUTHORIZED_KEYS:-}" ]]; then
        printf '%s\n' "$INFRA_SSH_AUTHORIZED_KEYS" \
            > /home/infra/.ssh/authorized_keys
        chown infra:infra /home/infra/.ssh/authorized_keys
        chmod 0600 /home/infra/.ssh/authorized_keys
    fi

    if [[ -f /home/infra/.ssh/authorized_keys ]]; then
        sed -i \
            -e 's/^#\?PubkeyAuthentication.*/PubkeyAuthentication yes/' \
            -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' \
            /etc/ssh/sshd_config
    elif [[ -n "${INFRA_SSH_PASSWORD:-}" || "${ALLOW_DEFAULT_LAB_PASSWORD:-false}" == "true" ]]; then
        sed -i \
            -e 's/^#\?PasswordAuthentication.*/PasswordAuthentication yes/' \
            /etc/ssh/sshd_config
    fi

    /usr/sbin/sshd
fi

if [[ ! -f "$CONFIG" ]]; then
    echo "ERREUR: configuration agent absente: $CONFIG" >&2
    exit 1
fi

if [[ -z "${INFRAFLOW_AGENT_TOKEN:-}" ]]; then
    echo "ERREUR: INFRAFLOW_AGENT_TOKEN doit être défini." >&2
    exit 1
fi

# Le binaire reste le processus principal du conteneur.
exec /usr/local/bin/infraflow-agent run -config "$CONFIG" "$@"