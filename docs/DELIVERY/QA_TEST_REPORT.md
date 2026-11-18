# QA test report

**Revision under test:** source based on `develop` HEAD `9707df0`, plus the
three Agent changes listed below. Source changes remain uncommitted.

Machine-readable sanitized results are in
[`artifacts/delivery/qa-summary.json`](../../artifacts/delivery/qa-summary.json).

## Change under test

- `agent/internal/adapters/filesystem/state.go`: permit verified
  `native_device_config` and `native_manifest` artifacts in stored paths.
- `agent/internal/adapters/filesystem/state_test.go`: test saving and reopening
  both native artifact types with SHA-256 validation.
- `agent/internal/adapters/processor/generic_test.go`: assert that native
  artifacts are staged and not executed.

## Test results

| Check | Result |
|---|---|
| Targeted Agent filesystem, processor, and application tests | PASS |
| Full `go test -count=1 ./...` | PASS |
| Full `go test -race -count=1 ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./provider/cmd ./agent/cmd` | PASS |
| `go mod verify` | PASS |
| CI-pinned `govulncheck` | PASS; zero reachable vulnerabilities |
| Agent processor coverage | PASS; 93.2% (90% CI threshold) |
| Agent CLI coverage | PASS; 66.9% (65% CI threshold) |
| Presentation demo and artifact verifier | PASS; hashes, paths, sizes, bootstrap opt-in/out, unsupported OSPF |
| GNS3 real Provider–Agent acceptance run | PASS; 13/13 artifacts complete |
| GitHub Actions on this uncommitted revision | Not available |

The full suite was run in an isolated worktree containing the same three source
changes, so tracked output files in the user's primary checkout were not
rewritten by tests.

## Notes

The successful GNS3 report supersedes the initial failed artifact staging
attempt only for the repaired run; both records are retained. `govulncheck`
reported zero reachable vulnerabilities; it also listed one finding in
imported code and five in required modules that it could not reach from this
program. The GNS3 test does not claim vendor parser, device, ZTP, AutoInstall,
Terraform, or bootstrap service coverage. Windows execution and Windows CI are
not claimed.
