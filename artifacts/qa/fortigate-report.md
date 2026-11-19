# FortiGate / FortiOS — 2026-11-19

## Result: BLOCKED (no device login)

The local image `fortigate-7.0.8-gns3.qcow2` booted as `FortiOS-VM64-KVM` in the
isolated project (linked clone; the base image was not modified). The console
reported *"The config file may contain errors"* and the image already carries an
admin password: the factory-default empty password and the lab default were
both rejected. No further guessing or password-recovery bypass was attempted on
the user's image. Port1 sent no DHCP traffic during ~5 min (pre-set static
configuration), so the FortiManager-discovery DHCP options (240/241) could not
be exercised against the device either.

**To unblock:** provide the FortiGate admin password for this image (through the
environment, not the repository), or a fresh FortiOS 7.0.x VM image.

## What was verified without the device

| Item | Status | Evidence |
|---|---|---|
| `fortios-native` rendering for `FortiGate-VM64-KVM` 7.0.8 (hostname, two static interfaces with `set status up`, static route with explicit sequence) | Generated, **not applied** | [device-fgt1.cfg](evidence/fortigate/device-fgt1.cfg) sha256 `b4e5c1c1…2a08`; `go test ./internal/adapters/generation -run FortiOS` |
| Static route without `fortinet_sequence` | Rejected at generation with an explicit message | lab run |
| FortiManager ZTP (DHCP 240/241) | Unit-tested in the DHCP server; PARTIAL (requires FortiManager) | `agent/internal/delivery/dhcp` tests |
| FortiDeploy / FortiZTP cloud | UNSUPPORTED (external entitlement) | `internal/domain/bootstrap_profile.go` |

No FortiOS capability is claimed beyond rendering.
