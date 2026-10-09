# InfraFlow Agent

The agent is an independent service with its own YAML deployment configuration. It authenticates to the provider over gRPC, streams only manifest-published artifacts to disk with hash verification, processes supported inventory/topology state, and reports results. It does not generate provider files or provision devices. Experimental DHCPv4 and read-only TFTP/HTTP bootstrap servers can be started separately from provider-generated configs; DNS service and vendor-specific provisioning are not active agent capabilities.

Those bootstrap services are planned as local, isolated agent capabilities. The
provider can now generate static DHCP/DNS/TFTP/PXE/iPXE contracts, but the agent
does not apply or run most of them yet. DHCPv4 is experimental: leases are
held in memory, so restarting forgets allocations, and the server has not been
validated on an isolated physical network or with vendor-specific clients. Do
not run it on a production or shared LAN. Its command requires an explicit
interface and IPv4 address assigned to that interface; UDP port 67 usually
requires root or Linux `CAP_NET_BIND_SERVICE`.

```sh
INFRAFLOW_AGENT_TOKEN='<same secret configured on provider>' \
  go run ./agent/cmd run -config examples/agent-config.yaml
```

To run DHCP on an isolated lab segment, explicitly enable it in the site config
(`services.dhcp: true`), generate bootstrap artifacts, then start the agent
command explicitly:

```sh
go run ./agent/cmd serve-dhcp \
  -config generated/lab/bootstrap/dhcp/config.json \
  -interface eth1 \
  -listen 192.168.100.1
```

The service rejects disabled configs, invalid or overlapping pools, and listen
addresses outside the configured network or inside an assignable pool. Stop it
with `Ctrl+C` or `SIGTERM`. Use a disposable isolated network until persistent
leases and lab validation are implemented.

Enable `services.tftp: true` to opt in to TFTP in the generated config. The
generated DHCP contract advertises the TFTP server in option 66, the
configured boot filename (currently `undionly.kpxe`) in option 67, and the
agent address as `next-server`. The agent does not bundle iPXE firmware files;
place approved `undionly.kpxe` and/or `ipxe.efi` files in the configured
`tftp/` runtime directory. TFTP is unauthenticated and read-only, so bind it
only to the isolated provisioning interface:

```sh
go run ./agent/cmd serve-tftp \
  -config /var/lib/infraflow-agent/lab/bootstrap/tftp/config.json \
  -root /var/lib/infraflow-agent/lab/bootstrap \
  -interface eth1 \
  -listen 192.168.100.1
```

The generated iPXE script downloads published scripts over HTTP. Serve only
the verified bootstrap artifact paths on the isolated interface:

```sh
go run ./agent/cmd serve-bootstrap \
  -directory /var/lib/infraflow-agent \
  -interface eth1 \
  -listen 192.168.100.1
```

The agent can run its current read-only Ansible inspection and validate the
current data-only Terraform declaration. The Ansible runner accepts only the
generated debug task and forces check mode. Terraform rejects provider,
resource, module, and data blocks, then runs `init -backend=false` and
`validate`; it does not plan or apply infrastructure.

```sh
go run ./agent/cmd execute-ansible \
  -directory /var/lib/infraflow-agent \
  -site lab \
  -config /etc/infraflow/agent.yaml

go run ./agent/cmd validate-terraform \
  -directory /var/lib/infraflow-agent \
  -site lab \
  -config /etc/infraflow/agent.yaml
```

Both commands require the corresponding provider-generated artifacts to have
been downloaded by `agent run`. They time out and stage only the expected
files in a temporary workspace. Install `ansible-playbook` and `terraform` on
the agent host to use them. stdout and stderr are captured and displayed
incrementally on their respective CLI streams, with bounded memory and secret
redaction. `-config` is optional; when supplied, its configured agent identity,
token, logger, outbox, and existing gRPC `ReportLogs` call are used to persist
and synchronize process events. The agent must already be registered with the
provider. Without `-config`, process output is local to the command and is not
sent to the dashboard.

The Linux TUI is a separate human client of the provider HTTP API:

```sh
export INFRAFLOW_TUI_PASSWORD="$(./provider-data/get-admin-password.sh)"
go run ./agent/cmd tui \
  -address https://10.0.0.5:8080 \
  -username admin \
  -ca-file /etc/infraflow/provider-ca.crt
```

Use `r`, `j`, `a`, `e`, `l`, `h`, or `q` to refresh jobs, inspect jobs, agents,
recent backend-reported results, or a point-in-time page of technical logs,
show help, or quit. Event and technical-log inspection are admin-only. The TUI
does not stream continuously. The server address and port are command-line
editable. The TUI never displays the full session token or password. A
generation result is not an execution or verification result.

Edit `examples/agent-config.yaml` for the provider host/port, optional loopback
HTTP API address, site identity, local state directory, and TLS CA. When
`provider.api_address` is configured, the agent registers and sends a heartbeat
after its local artifact run. The HTTP registration path is loopback-only until
an HTTPS API is implemented. Remote/cloud gRPC endpoints require TLS; the
agent imports no provider Go package.
