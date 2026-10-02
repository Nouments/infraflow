# InfraFlow Provider

The provider is an independent service. It validates infrastructure YAML, creates plans, generates deterministic files, and serves manifest-listed artifacts plus state-report RPCs to authenticated agents over gRPC.

```sh
go run ./provider/cmd validate -f examples/infra.yaml
go run ./provider/cmd plan -f examples/infra.yaml
go run ./provider/cmd generate -f examples/infra.yaml -out ./provider-data
INFRAFLOW_AGENT_TOKEN='<secret of at least 32 bytes>' go run ./provider/cmd serve -config examples/provider-config.yaml
```

Edit `examples/provider-config.yaml` for the artifact directory, listen address, chunk size, and TLS certificate/key paths. Plaintext is allowed only on loopback. The bearer token value is read from the configured environment variable and is never stored in YAML.