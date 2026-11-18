# Local GNS3 test environment

**Capture:** 2026-11-18 19:30 UTC, unless stated otherwise.
**GNS3:** server `3.1.0a4`; live OpenAPI document reported schema `3.0.0` and
118 paths. Controller API was accessed locally at `http://127.0.0.1:3080/v3`.
Authentication was enabled; credentials and tokens are intentionally omitted.

## Test project

- Project: `infraflow-appliance-lab-20261118`
- Project ID: `ca690f7a-44e0-4af9-bb1d-813862cecf50`
- Final status: saved and closed; available to reopen in the local GNS3 GUI.
- Nodes: Provider Docker appliance, Agent Docker appliance, isolated Ethernet
  switch; two links connect each appliance to the switch.
- No Cloud or NAT node was present.
- Provider image: `infraflow-provider:delivery-20261118`, image ID
  `sha256:a824508d81cb0a54c344eecd43a14be4e20ab5eaa0f6437ae273efdfd363452e`.
- Agent image: `infraflow-agent:delivery-20261118-r1`, image ID
  `sha256:6f2c5fda5626755002c9b6f197a8ef74e9f3032fef450215fcc3144ee3857c69`.
- New delivery template IDs: Provider
  `db3c7d3c-360e-4929-998f-08cade0c8352`; Agent
  `5e5b401f-178c-447d-ae0d-77594a106832`.

The tested node addresses were Provider `192.168.0.3/24` and Agent
`192.168.0.2/24`, on the isolated switch. Provider HTTPS/API was reachable at
`https://192.168.0.3:8080`; Provider gRPC used `192.168.0.3:8443`. TLS
verification used the lab CA mounted from a private directory outside the
repository.

## Host snapshot

At the capture time the local catalog listed 221 appliances and 37 templates,
and 36 local Docker images. GNS3 reported 28 nodes, 26 links, seven projects
(six pre-existing and this new project), 66% memory usage, and 68% disk usage.
These are point-in-time readings, not current host health. Six projects
pre-dated this test; none was modified. The existing `topologie` project was
observed open and left untouched.

The report also showed a remote compute named `debian-server` as disconnected.
It was not used. Resource details and full host paths are omitted from this
document; the sanitized snapshot is in
[`provider-agent-run.json`](../../artifacts/delivery/gns3/provider-agent-run.json).

## Cleanup

After collecting evidence, the Provider and Agent were stopped. Their
test-token values were cleared from the saved node configuration, and GNS3
closed the project and removed the stopped Docker containers. The project
topology remains saved. The generated local image tags remain available to
reopen and repeat the test after supplying fresh credentials.

No GNS3 server settings, pre-existing projects, or pre-existing templates were
changed. No GNS3 client or API implementation was added to the InfraFlow
repository; controller access was test setup/observation only.
