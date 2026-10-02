# InfraFlow Agent

The agent is an independent service with its own YAML deployment configuration. It authenticates to the provider over gRPC, streams only manifest-published artifacts to disk with hash verification, processes supported inventory/topology state, and reports results. It does not currently generate provider files, provision devices, or run DHCP, DNS, TFTP, HTTP, or iPXE bootstrap services.

Those bootstrap services are planned as local, isolated agent capabilities. They are distinct from static DHCP/DNS/PXE artifact generation and will remain unsupported until each service has an adapter, restricted workspace behavior, fixtures, automated tests, and an observable result.

```sh
INFRAFLOW_AGENT_TOKEN='<same secret configured on provider>' \
  go run ./agent/cmd run -config examples/agent-config.yaml
```

Edit `examples/agent-config.yaml` for the provider host/port, optional loopback
HTTP API address, site identity, local state directory, and TLS CA. When
`provider.api_address` is configured, the agent registers and sends a heartbeat
after its local artifact run. The HTTP registration path is loopback-only until
an HTTPS API is implemented. Remote/cloud gRPC endpoints require TLS; the
agent imports no provider Go package.
