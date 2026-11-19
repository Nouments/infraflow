# Test matrix — 2026-11-19

Legend: **I** implemented · **U** unit-tested · **R** tested on a real virtual
appliance · **–** not tested · **X** unsupported by design.

## Automated gates (local, Go 1.27.2 `GOTOOLCHAIN=local`)

| Gate | Command | Result |
|---|---|---|
| Format | `gofmt -l $(git ls-files '*.go')` | clean |
| Vet | `go vet ./...` | pass |
| Unit/integration | `go test -count=1 ./...` | pass (all packages) |
| Race | `go test -race -count=1 ./...` | pass |
| Build | `go build ./provider/cmd ./agent/cmd` | pass |
| Vulnerabilities | `govulncheck ./...` | 0 reachable |
| Agent coverage | processor / CLI | 93.2% (≥90) / 72.9% (≥65) |
| Admin Web scripts | `node --check` ×2, `node --test log-stream.test.cjs` | pass, 4/4 |
| Shell | `bash scripts/cisco-ios-preflight_test.sh` | pass |
| Whitespace | `git diff --check` | clean |
| Hosted CI | `.github/workflows/go-ci.yml` | restored; see RELEASE_READINESS for the run status |

New regression tests added today: `TestGenerateNativeSelectsExactCiscoIOSvAutoInstallTemplate`,
`TestGenerateNativeRouterOSAutoRSCAcceptsConcreteReleaseOfDeclaredTrain`,
`TestMatchesReleaseTrainIsNumericAndPrefixBound`,
`TestKnownHostsFileVerifierAppliesFileChangesWithoutRestart`,
`TestExecutionServiceReportsExecutorValidationReason`,
`TestStandaloneServiceLinesDiagnoseIgnoredOffers`,
`TestStandaloneServiceLinesConfirmLeaseAndTransfer`,
`TestLocalFlashFileNotFoundIsNotATFTPFailure`, plus two IOSv domain-profile cases.

## Capability matrix

| Capability | Cisco 7200 12.4(24)T5 | Cisco IOSv 15.9(3)M6 | MikroTik CHR 7.8 | FortiOS VM 7.0.8 |
|---|---|---|---|---|
| Native config generation | I U R | I U R | I U R | I U (not applied) |
| InfraFlow DHCP lease | R (prior) | R (standard client) | R | – (no DHCP from device) |
| InfraFlow TFTP delivery | R (prior) | R | n/a | – |
| DHCP-triggered Cisco AutoInstall | R PASS (prior) | R **FAIL – appliance** | n/a | n/a |
| Cisco ZTP | X | X | n/a | n/a |
| RouterOS `.auto.rsc` Auto Import | n/a | n/a | I U R (upload by harness) | n/a |
| InfraFlow-driven RouterOS upload / `.auto.log` | n/a | n/a | – (not implemented) | n/a |
| FortiManager ZTP options 240/241 | n/a | n/a | n/a | I U |
| FortiDeploy | n/a | n/a | n/a | X |
| Static route applied + forwarding | – | R | R | – |
| OSPF config applied | – | – | R (no neighbor) | – |
| `cisco.ios.configure` SSH executor | – | I U **R** | n/a | n/a |
| Host-key fail-closed | U | U **R** | n/a | n/a |
| Persistence | not saved (by design) | not saved (by design) | R (auto, survives reboot) | – |
| Idempotent re-apply | – | R (executor read-back) | R | – |
| Diagnostics on real evidence | R (prior) | R | – | – |

## Failure scenarios

| Scenario | How | Result |
|---|---|---|
| DHCP unavailable | IOSv boot with no server | 7 unanswered DISCOVERs/45 s (R) |
| Client ignores offers | IOSv AutoInstall | diagnosed `DHCP_OFFER_NOT_ACCEPTED` (R) |
| TFTP file missing / denied / failed | unit tests | U |
| Invalid configuration | unit tests (`DEVICE_CONFIG_REJECTED`) | U |
| Wrong SSH password | real IOSv | auth error surfaced (R) |
| Wrong / rotated host key | real IOSv | fails closed (R) |
| Unauthorized device | real Provider | refused pre-connection (R) |
| Invalid task parameters | real Provider | specific reason returned (R, after fix) |
| Provider/Agent restart, offline, heartbeat loss | prior session (`agent-status-lifecycle.txt`, `offline-acceptance.txt`) | R (prior) |
