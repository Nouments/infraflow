# InfraFlow Agent

The agent is an independent service with its own YAML deployment configuration. It authenticates to the provider over gRPC, streams only manifest-published artifacts to disk with hash verification, processes supported inventory/topology state, and reports results. It does not generate provider files or provision devices.

```sh
INFRAFLOW_AGENT_TOKEN='<same secret configured on provider>' \
  go run ./agent/cmd run -config examples/agent-config.yaml
```

Edit `examples/agent-config.yaml` for the provider host/port, local state directory, and TLS CA. Remote/cloud endpoints require TLS; plaintext is accepted only for loopback development. The agent imports no provider Go package.