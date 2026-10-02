# InfraFlow Provider

The provider is an independent service. It validates infrastructure YAML, creates plans, generates deterministic files, and serves manifest-listed artifacts plus state-report RPCs to authenticated agents over gRPC.

```sh
go run ./provider/cmd validate -f examples/infra.yaml
go run ./provider/cmd plan -f examples/infra.yaml
go run ./provider/cmd generate -f examples/infra.yaml -out ./provider-data
INFRAFLOW_AGENT_TOKEN='<secret of at least 32 bytes>' go run ./provider/cmd serve -config examples/provider-config.yaml
```

Edit `examples/provider-config.yaml` for the artifact directory, gRPC listen address, optional loopback HTTP API address, chunk size, and TLS certificate/key paths. Plaintext is allowed only on loopback. The bearer token value is read from the configured environment variable and is never stored in YAML.

The optional HTTP API exposes authenticated planning jobs and agent registration/heartbeat state:

```sh
curl -H "Authorization: Bearer $INFRAFLOW_AGENT_TOKEN" \
  http://127.0.0.1:8080/api/v1/jobs
```

Agents register with `POST /api/v1/agents/register` and send heartbeats to
`POST /api/v1/agents/{agent_id}/heartbeat`. Agent identity is bound to its
first site, persisted under the private artifact workspace, and cannot be
silently rebound by a later request. Requests are bounded, reject unknown JSON
fields, and use the same bearer token as the gRPC service.

```sh
curl -X POST -H "Authorization: Bearer $INFRAFLOW_AGENT_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary '{"input":"sites:\n  - name: lab\n"}' \
  http://127.0.0.1:8080/api/v1/jobs
```

Jobs validate and persist a plan. They do not execute provisioning; device tasks remain explicitly blocked until a verified adapter exists. DHCP, DNS, TFTP, HTTP, and iPXE are agent-side bootstrap services and are not provided by this planning API.
