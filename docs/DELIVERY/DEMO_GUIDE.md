# Demo guide: Provider and Agent appliances

This demo uses the standard InfraFlow Provider and Agent Docker images as two
separate appliances. GNS3 is optional external lab tooling; InfraFlow has no
GNS3 runtime dependency or API client.

## Build the appliances

From the repository root:

```sh
docker build -f docker/infraflow-provider/Dockerfile -t infraflow-provider:local .
docker build -f docker/infraflow-agent/Dockerfile -t infraflow-agent:local .
```

Prepare a Provider config, its TLS certificate/key, an Agent CA file, and
fresh test credentials in a private directory. Do not put secrets in source
control, command history, screenshots, or captured logs. Set the Provider and
Agent network addresses to match the chosen isolated lab segment.

## Connect and run

In a local GNS3 project, add one Provider appliance, one Agent appliance, and
an isolated Ethernet switch. Connect both appliances to that switch. Keep
Cloud/NAT and external networks out of the lab. Configure the required
Provider and Agent environment values using the local GNS3 UI or other
authorized test tooling; the repository does not automate GNS3.

Start Provider first, then Agent. Check Provider readiness, confirm Agent
registration for the intended site, and inspect the report for artifact counts,
hashes, and failures. Verify that native configuration is present in Agent
state and compare it to Provider output. Do not call this device configuration
unless a separate authorized device test actually applies and reads back the
configuration.

## Cleanup

Stop both appliances, clear test credentials from saved node properties, and
close the project so the GNS3 Docker containers are removed. Keep the saved
topology if it is useful for another run. Rotate test credentials after any
log or session token exposure.

## Reproduced run

The 2026-11-18 run completed 13/13 artifacts and persisted the report. Detailed
topology, results, and limits are in
[`GNS3_ACCEPTANCE_TESTS.md`](GNS3_ACCEPTANCE_TESTS.md).
