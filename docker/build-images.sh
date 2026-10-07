#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

echo "Building InfraFlow provider image..."
docker build -t infraflow-provider:dev -f Dockerfile.provider .

echo "Building InfraFlow agent image..."
docker build -t infraflow-agent:dev -f Dockerfile.agent .

echo "Images ready:"
docker images --format '{{.Repository}}:{{.Tag}}' | grep '^infraflow-.*:dev$' || true
