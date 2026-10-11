#!/usr/bin/env bash
set -Eeuo pipefail

CONFIG="${INFRAFLOW_PROVIDER_CONFIG:-/etc/infraflow/provider-config.yaml}"
DATA="${INFRAFLOW_PROVIDER_DATA:-/var/lib/infraflow-provider}"

mkdir -p "$DATA"
chmod 0750 "$DATA"

if [[ ! -f "$CONFIG" ]]; then
    echo "ERREUR: configuration provider absente: $CONFIG" >&2
    exit 1
fi

if [[ -z "${INFRAFLOW_AGENT_TOKEN:-}" ]]; then
    echo "ERREUR: INFRAFLOW_AGENT_TOKEN doit être défini." >&2
    exit 1
fi

# Les fichiers de données doivent rester persistants entre les redémarrages.
# La configuration de référence utilise des chemins relatifs à ./provider-data.
# On lance donc le provider depuis le répertoire de données.
cd "$DATA"

# Le fichier de configuration est monté ou fourni séparément.
# Les chemins TLS distants doivent être configurés dans ce YAML.
exec /usr/local/bin/infraflow-provider serve -config "$CONFIG" "$@"