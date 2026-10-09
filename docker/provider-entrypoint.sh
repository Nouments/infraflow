#!/bin/sh
set -eu

mkdir -p /data/provider /etc/infraflow/tls

if [ ! -f /etc/infraflow/tls/ca.key ] || [ ! -f /etc/infraflow/tls/ca.crt ]; then
  echo "Generating local CA for InfraFlow provider"
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout /etc/infraflow/tls/ca.key \
    -out /etc/infraflow/tls/ca.crt \
    -days 3650 \
    -subj "/CN=InfraFlow Local Lab CA"
fi

if [ ! -f /etc/infraflow/tls/server.key ] || [ ! -f /etc/infraflow/tls/server.crt ]; then
  echo "Generating signed provider certificate"
  openssl genrsa -out /etc/infraflow/tls/server.key 2048
  openssl req -new -key /etc/infraflow/tls/server.key \
    -subj "/CN=infraflow-provider" \
    -out /etc/infraflow/tls/server.csr
  openssl x509 -req -in /etc/infraflow/tls/server.csr \
    -CA /etc/infraflow/tls/ca.crt \
    -CAkey /etc/infraflow/tls/ca.key \
    -CAcreateserial \
    -out /etc/infraflow/tls/server.crt \
    -days 825 -sha256
  rm -f /etc/infraflow/tls/server.csr
fi

exec /usr/local/bin/infraflow-provider "$@"
