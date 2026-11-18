# Sanitized GNS3 evidence

Evidence for the local Provider–Agent appliance acceptance run on
2026-11-18. Credentials, tokens, private keys, and full host paths are omitted.

| File | Contents |
|---|---|
| [`provider-agent-run.json`](provider-agent-run.json) | Sanitized GNS3 version/project snapshot, appliance image IDs, topology, and successful run summary. |
| [`agent-report-summary.json`](agent-report-summary.json) | Agent report with 13 completed artifacts and hashes. |
| [`agent-console.log`](agent-console.log) | Sanitized Agent output from the successful run. |
| [`provider-console.log`](provider-console.log) | Sanitized Provider output from the successful run. |
| [`initial-agent-sync-failure.log`](initial-agent-sync-failure.log) | Initial failure that exposed the native artifact allowlist omission. |
| [`cleanup.json`](cleanup.json) | Post-run project/token/container cleanup status. |

The successful report ID is
`db4385b701279fc45c870bb35dbd01402dedd351db51ac4ebe91113b861d661e`. The
project remains saved and closed after the run. No Cisco router was used and no
device configuration was applied.
