#!/bin/sh
set -eu

mkdir -p /data/agent /etc/infraflow/tls

if [ ! -f /etc/infraflow/tls/ca.crt ]; then
  echo "Waiting for shared CA certificate from the provider container" >&2
  sleep 5
fi

exec /usr/local/bin/infraflow-agent "$@"
