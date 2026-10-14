# InfraFlow — Bloc 1 Audit

Audit date: 2026-10-13
Branch: `develop`
Reference commit inspected before edits: `a4b7cb07605be5503c608e97343ab9f78348bbfe`
Audit correction commit: `0c3cd1c05347a602f18c786a4fc53a36e4137ed0` (`fix(audit): harden generation and artifact integrity`)
Initial worktree: only `current_step.md` was modified; it contained the user's audit prompt and was replaced with the requested operational status. After that audit commit, a separate user edit appeared in `provider/internal/delivery/grpcapi/server_test.go` (import alias only); it remains uncommitted and was not modified or staged during this QA reproduction.

## Scope and decision

The audit covered YAML parsing and network validation, planning, deterministic generation, generic and vendor Ansible, Terraform, manifests/catalog integrity, Provider publication and execution, HTTP/gRPC auth, Agent artifact processing/reporting/TUI, Web console provenance, Cisco IOS SSH execution, logging/security, tests, and current documentation. No live Cisco/GNS3 session occurred.

Decision: **BLOC 1 INCOMPLET — PREUVES MANQUANTES**. Confirmed code defects found in this scope were corrected and repository checks pass. Ansible collection-level syntax validation and authorized device/lab verification remain unexecuted; neither is represented as a pass. Bloc 2 was not started. InfraFlow is not declared production-ready.

## QA matrix

| Component | Status and files inspected | Relevant tests | Findings, evidence, and limits |
| --- | --- | --- | --- |
| YAML parsing, normalization, network validation | Implemented: `internal/adapters/config/load.go` | `internal/adapters/config/load_test.go`, `internal/adapters/config/config_test.go` | Strict known-field, one-document YAML parsing; endpoint normalization; IPv4/CIDR, MAC, duplicate, overlap, route and NAT checks. Full Go suite passed. |
| Planning and intent classification | Implemented: `internal/application/planner/planner.go`, `provider/internal/application/plan_generation.go` | `internal/application/planner/planner_test.go`, `provider/internal/application/service_test.go` | Stable plan IDs/order and `PLANNED` lifecycle are tested. Fixed silent route/NAT removal in interface-only `PlanAndGenerate`: such requests now return `UNSUPPORTED` and publish no vendor artifacts. |
| Inventory, topology, bootstrap, manifests | Implemented: `internal/adapters/generation/generator.go`, `internal/adapters/generation/bootstrap.go` | `internal/adapters/generation/generator_test.go`, `internal/adapters/generation/catalog_test.go` | Deterministic output, canonical ordering, multi-site generation, symlink rejection and combined manifest publication are test-backed. Generation does not imply execution. |
| Generic Ansible | Implemented as inventory plus metadata inspection only: `internal/adapters/generation/generator.go` | `internal/adapters/generation/generator_test.go`, `agent/internal/application/toolrunner/runner_test.go` | `site.yml` contains only `ansible.builtin.debug`; normal Agent processing stages artifacts, and the optional inspection runner forces `--check`. Fixed the ambiguous artifact metadata: `site.yml` is now `ansible_inspection_playbook`, not the vendor configuration type. YAML parsing/determinism tests pass. |
| Vendor Ansible profiles | Implemented, rendered and explicitly `UNVERIFIED`: `internal/adapters/generation/vendor_ansible.go`, `internal/domain/model.go` | `internal/adapters/generation/vendor_ansible_test.go`, `internal/domain/vendor_capabilities_test.go` | Profiles are Cisco IOS-XE/SSH, MikroTik RouterOS/API, and FortiOS/HTTPS. Fixed FortiOS module argument/loop placement, invented `port1`/`allowaccess`, Cisco invented `enabled`, DHCP omission, and transport mismatches. Unsupported NAT/route/method intent is rejected or classified unsupported. Mutation requires check mode or `INFRAFLOW_APPLY=true`; generation is manual-only and not lab-verified. Tests parse generated YAML, check connection metadata, hashes and determinism. |
| Terraform | Implemented as data-only output: `internal/adapters/generation/generator.go` | `internal/adapters/generation/generator_test.go`, `agent/internal/application/toolrunner/runner_test.go` | Seven deterministic files model normalized desired state; no provider/resource provisioning is declared. Terraform CLI was unavailable, so `terraform validate` was not run. |
| Artifact catalog, paths, hashes, publication | Implemented: `internal/adapters/generation/catalog.go`, `pkg/protocol/artifact.go`, `internal/infrastructure/safefs/atomic.go` | `internal/adapters/generation/catalog_test.go`, `internal/infrastructure/safefs/atomic_test.go`, `pkg/protocol/artifact_test.go` | Fixed catalog listing of altered bytes: `Catalog` now hashes content before publishing it, and reads use `safefs.OpenReadOnly`. Provider delivery uses the safe opener; gRPC preserves `DataLoss` on hash mismatch. Tests cover tampering, paths and normal reads. |
| Provider planning/generation/execution | Implemented: `provider/internal/application/service.go`, `provider/internal/application/plan_generation.go`, `provider/internal/application/executions.go`, `internal/application/scheduler/scheduler.go` | `provider/internal/application/service_test.go`, `provider/internal/delivery/httpapi/jobs_test.go`, `internal/application/scheduler/scheduler_test.go` | Created, queued, running, succeeded, failed, cancelled and timed-out states are distinct. Generation returns `NOT_REQUESTED` execution and `NOT_PERFORMED` verification; Cisco's `Verified` result is produced only after read-back. Tests passed. |
| HTTP API, authentication, authorization | Implemented: `provider/internal/delivery/httpapi/jobs.go`, `provider/internal/application/auth.go`, `provider/internal/adapters/sqlite/users.go` | `provider/internal/delivery/httpapi/jobs_test.go`, `provider/internal/adapters/sqlite/users_test.go` | Human sessions/RBAC are separate from the Agent bearer token; execution/log administration is admin-only. Tests cover auth, role restrictions, invalid headers, planning and execution APIs. |
| gRPC, Agent artifact lifecycle and reports | Implemented: `provider/internal/delivery/grpcapi/server.go`, `agent/internal/application/runner.go`, `agent/internal/adapters/providergrpc/client.go`, `agent/internal/adapters/filesystem/state.go` | `provider/internal/delivery/grpcapi/server_test.go`, `agent/internal/application/runner_test.go`, `agent/internal/adapters/filesystem/state_test.go` | Catalog and report use the machine token. Agent download verifies hashes before atomic storage; report `completed` means artifact processing/staging, not device configuration. Tampered download is rejected. |
| Agent TUI/dashboard | Implemented: `agent/internal/delivery/tui/model.go` | `agent/internal/delivery/tui/model_test.go` | Views use API/model state; tests reject fake IP and uptime values and cover role-based navigation. This does not evidence live device state. |
| Provider Web dashboard and provenance | Implemented: `provider/internal/delivery/web/app.go`, `provider/internal/delivery/web/assets/app.js` | `provider/internal/delivery/web/app_test.go`, `provider/internal/delivery/httpapi/jobs_test.go` | Tests cover auth, assets, responsive CSS, no fake execution records, and distinct generation/execution/verification statuses. UI tests are not browser E2E or lab evidence. |
| Cisco `cisco.ios.configure` | Implemented over SSH: `provider/internal/adapters/executors/cisco_ios_configure.go`, `cisco_ios_transport.go`, `hostkey.go`, `device_target.go`, `provider/internal/infrastructure/credentials/` | `provider/internal/adapters/executors/cisco_ios_configure_test.go`, `cisco_ios_transport_test.go`, `device_target_test.go` | Strict JSON/field, host/interface/CIDR/admin-state checks; authorized target and credential resolvers; strict host-key verification; bounded command output/timeouts; secret redaction; read-back verification; no automatic retry or `write memory`. Tests use mocks/local transports, not a router. Lab status: **NON EXÉCUTÉ**. |
| Logs, audit, input safety | Implemented: `pkg/observability/logger.go`, `pkg/observability/store.go`, `provider/internal/delivery/httpapi/logs.go`, `internal/infrastructure/security/token.go` | `pkg/observability/logger_test.go`, `pkg/observability/store_test.go`, `provider/internal/delivery/httpapi/jobs_test.go`, `internal/infrastructure/security/token_test.go` | Bounded/rotated structured logs and secret redaction are tested; API log reads are admin-only and ingestion is correlated. No confirmed P0/P1 issue remained. |
| Documentation and build/test contracts | Inspected and updated: `README.md`, `ARCHITECTURE.md`, `INFRAFLOW_SPEC.md`, `provider/README.md`, `agent/README.md`, `Makefile` | Repository Go tests and Makefile gates below | Docs now distinguish inspection playbooks from vendor configuration and data-only Terraform. No Ansible CLI was available for `--syntax-check`; no Terraform CLI was available for `validate`. |

## Confirmed defects and corrections

- **P1, generation reporting:** interface-only `PlanAndGenerate` silently removed routes/NAT and could mark the remaining interface task `GENERATED`. It now reports unsupported intent and emits no artifacts. Regression test: `TestPlanAndGenerateRejectsIntentOutsidePlannedInterfaceTask`.
- **P1, FortiOS renderer:** loop keys were nested in module arguments; route schema was malformed and hardcoded `port1`; interface output invented `allowaccess: ping`. Renderer now follows collection task structure and input route/interface fields. Regression coverage: `TestRenderFortinetTasksUsesCollectionSchemaAndExplicitRouteInterface` and three-profile YAML test.
- **P1, artifact availability:** catalog could list file bytes that disagreed with the manifest. Catalog now checks SHA-256 before listing; safe reads are used by Provider delivery; gRPC maps mismatch to `DataLoss` before streaming. Regression coverage: `TestCatalogRejectsTamperedArtifact`, `TestGRPCDoesNotCommitTamperedArtifactStream`, artifact verification integration.
- **P2, profile correctness:** Cisco Ansible output invented `enabled: true`, omitted explicit DHCP intent, and accepted `netconf` despite using `network_cli`. Cisco Ansible now uses SSH/network_cli and renders only declared addressing; transport methods are restricted to Cisco SSH, RouterOS API and FortiOS HTTPS.
- **P2, provenance ambiguity:** generic debug `site.yml` and mutating vendor playbooks shared the `ansible_playbook` type, and `GenerateAnsible` also wrote vendor artifacts. Generic and vendor generators are now separate; `GenerateAll` explicitly invokes both; generic metadata uses `ansible_inspection_playbook`.
- No confirmed P0 defect. No Cisco/GNS3 execution was used to justify a success claim.

## Commands executed in this session

Repository baseline before the audit correction commit:

```text
git branch --show-current -> develop (exit 0)
git status --short -> M current_step.md (exit 0; pre-existing user edit)
git log -5 --oneline -> a4b7cb0 (HEAD/origin/develop), b584461, 77e2d4e, 0be997c, e73b3bf (exit 0)
git rev-parse HEAD -> a4b7cb07605be5503c608e97343ab9f78348bbfe (exit 0)
```

Final repository gates executed after source corrections:

```text
go test -count=1 ./... -> PASS (exit 0)
make fmt-check -> PASS (exit 0)
make vet -> PASS (exit 0)
make build -> PASS (exit 0; Provider and Agent binaries built)
git diff --check -> PASS (exit 0; printed explicit PASS marker)
```

`git diff --check` before replacing the pre-existing `current_step.md` prompt reported trailing Markdown hard-break spaces in its metadata (exit 2). A check of the first replacement draft also found trailing spaces in this file's three metadata lines (exit 2); they were removed, and the final check passed.

Final Git review commands executed before commit:

```text
git status --short -> audit source, tests and docs modified; no untracked entries
git diff --stat -> 27 files changed, 701 insertions(+), 259 deletions(-)
git diff -- CURRENT.md current_step.md -> reviewed; status documents consistently say incomplete and Bloc 2 awaits approval
```

QA reproduction requested after the audit commit:

```text
git status --short --branch -> ## develop...origin/develop [ahead 1]; M provider/internal/delivery/grpcapi/server_test.go (exit 0)
git rev-parse HEAD -> 0c3cd1c05347a602f18c786a4fc53a36e4137ed0 (exit 0)
git --no-pager log -5 --oneline -> 0c3cd1c, a4b7cb0, b584461, 77e2d4e, 0be997c (exit 0)
go test -count=1 ./... -> PASS (exit 0)
make fmt-check -> PASS (exit 0; target confirmed in Makefile)
make vet -> PASS (exit 0; target confirmed in Makefile)
make build -> PASS (exit 0; target confirmed in Makefile; both binaries built)
git diff --check -> PASS (exit 0)
```

The audit correction commit contains 27 files (`701 insertions(+), 259 deletions(-)`). The post-commit user edit in `provider/internal/delivery/grpcapi/server_test.go` is intentionally excluded from it.

Regression probes were run before fixes and failed for the asserted defects: FortiOS renderer task shape, mixed generic/vendor output, Cisco method mismatch, route intent reported generated, and tampered catalog visibility. After the fixes, the affected package suites passed; the full Go suite above also passed. An initial full suite run exposed stale expectations for Cisco default method and artifact integrity error mapping; both contracts/tests were corrected and the full suite was rerun successfully.

## Checks not executed and limits

- `ansible-playbook --version` -> `zsh: command not found` (exit 127); `yamllint --version` -> `zsh: command not found` (exit 127). Generated YAML was parsed using the repository's `gopkg.in/yaml.v3` tests; this is not Ansible collection/module validation.
- `terraform version` -> `zsh: command not found` (exit 127). Go tests verify deterministic data-only output, not Terraform parser acceptance.
- Cisco IOS/GNS3 lab: **NON EXÉCUTÉ**; no authorized reachable endpoint, credentials, or lab evidence was available.
- Cisco SSH tests are mock-based and do not establish platform/version compatibility. Vendor Ansible manifests remain `UNVERIFIED`; startup-config saving remains disabled.

Residual risk: collection-specific runtime behavior, target model/firmware compatibility, check-mode behavior on actual equipment, and the Cisco executor's live CLI/read-back semantics require external validation. The score below does not replace those checks.

## Maturity score and next gate

**72/100 for the repository behavior and tests actually inspected.** The score credits strict input validation, deterministic artifacts, state separation, integrity checks, access control, and a passing Go/build/static baseline. It is reduced for unexecuted Ansible/Terraform CLI validation and absent lab/E2E evidence. It is not a production-readiness score.

**Decision: BLOC 1 INCOMPLET — PREUVES MANQUANTES.** The next step remains external syntax/runtime validation and QA review. Bloc 2 requires explicit Product Owner / QA approval.

## Bloc 2 verification addendum — 2026-10-14

This is a separate verification record; the Bloc 1 decision above remains unchanged.

- HEAD inspected: `3df0215185fec2e5f174a238bd257ec4250a7da1` on `develop`.
- Targeted artifact packages: `go test -count=1 ./pkg/protocol ./internal/adapters/generation ./provider/internal/adapters/filesystem ./provider/internal/adapters/executors ./provider/internal/delivery/grpcapi ./agent/internal/adapters/providergrpc ./agent/internal/delivery/artifacts` — exit 0.
- Full Go suite: `go test -count=1 ./...` — exit 0.
- `make fmt-check`, `make vet`, `make build`, and `go test -race -count=1 ./...` — each exit 0.
- Ansible commands are blocked: both `ansible-playbook --version` and `ansible-galaxy collection list` returned exit 127 (`zsh: command not found`). No syntax-check or collection validation was run.
- Terraform is blocked: `terraform version` returned exit 127 (`zsh: command not found`). `fmt`, `init`, and `validate` were not run; no `apply` was run. Inspected generated Terraform is data-only and declares no resources.
- Full `git diff --check` returns exit 2 because the pre-existing local `current_step.md` content has trailing spaces on lines 6–7. Those lines were preserved. The audit did not modify source code or tests and found no additional reproducible defect.
- This verification does not constitute Ansible/Terraform CLI validation, device/lab evidence, or PO/QA approval of Bloc 1. Bloc 3 was not started.

## Bloc 2 final validation addendum — 2026-10-14

This later pass supersedes only the external-tool-blocked findings above; the Bloc 1 decision remains **INCOMPLET — PREUVES MANQUANTES**.

- The original local whitespace finding was traced to the seven pre-existing A-G lines in the locally modified `current_step.md` mission text (now lines 148–154). They were preserved. Final `git diff --check` still exits 2 for those lines.
- A real `generate-all` run from an explicit temporary Cisco IOS-XE input initially failed because its fixture omitted the required bootstrap CIDR; adding the required CIDR made generation succeed. All outputs were written under `/tmp`.
- Ansible was installed in an isolated Python 3.14.7 venv as `ansible-core 2.21.5`. Generated requirements installed `cisco.ios 11.5.1`, `ansible.netcommon 8.7.1`, and `ansible.utils 6.1.1` into a temporary collection path. Both generated playbooks passed `ansible-playbook --syntax-check`; the vendor inventory passed `ansible-inventory --list`; `cisco.ios.ios_l3_interfaces` and `ansible.netcommon.network_cli` resolved. No playbook was executed and no device was contacted.
- Terraform v1.16.6 was downloaded from HashiCorp’s official release URL and its Linux AMD64 archive passed the official SHA-256 checksum. Generated-module `terraform fmt -check`, `terraform init -backend=false`, and `terraform validate` passed after fixing HCL scalar-key alignment. The generated module declares no resources or providers; no `apply` was run.
- The same Terraform checks also passed against an isolated module generated from the repository’s `examples/infra.yaml` input.
- Confirmed defect: generated `locals.tf` was valid enough to generate but failed Terraform’s formatter because object attributes were not aligned according to Terraform’s scalar groups. The renderer now aligns contiguous scalar values while leaving non-empty nested maps/lists as group boundaries; empty `{}` and `[]` remain inline. Regression coverage is in `TestGenerateTerraformIsDeterministicAndDataOnly`. An intermediate correction test caught and fixed a negative `strings.Repeat` count.
- Final `PAGER=cat go test -count=1 ./...`, `PAGER=cat make fmt-check`, `PAGER=cat make vet`, `PAGER=cat make build`, `PAGER=cat go test -race -count=1 ./...`, and the targeted artifact package tests each exited 0.
- No Cisco/GNS3 compatibility or real-device behavior is established. No Bloc 3 work was performed.

## Bloc 3 lab status — 2026-10-14

Bloc 3 was limited to a runbook and read-only preflight; no device configuration was attempted. Bloc 1 remains **INCOMPLET — PREUVES MANQUANTES**. The latest recorded Bloc 2 decision remains incomplete at its prior validation HEAD (`3df0215`); the Bloc 3 changes were committed on top of `96904f0` (commit `bb84b29`). QA review confirmed `git diff --check 96904f0 bb84b29` exits 0, but the full Bloc 2 Go suite was not rerun by QA on `bb84b29` (Go 1.25 was unavailable in the QA environment).

- Git was on `develop`, HEAD `96904f08a91967b3c7fd307bbc996bec8f0e2b16`; only `current_step.md` was modified before this pass.
- A local `gns3server` process/listener on port 3080 was observed. The QEMU process identified was named `fortigate-1` (excluded). `GET http://127.0.0.1:3080/v2/projects` returned HTTP 404; this is **not conclusive**, because GNS3 3.x serves its API under `/v3` (see the API probe in `docs/lab/cisco-ios-gns3.md`) and `/v3/projects` was not tried. No authorized Cisco IOS node, management IP, or independently verified SSH fingerprint was confirmed. No SSH or `/api/v1/executions` call was attempted.
- README addresses `192.168.0.2`/`.3` refer to Agent/Provider; `examples/cisco-ios-lab.yaml` labels `192.0.2.10` TEST-NET documentation. Neither is used as a router target.
- Cases A (hostname), B (Ethernet IPv4/admin-state), and C (invalid CIDR) are `NON EXÉCUTÉ`; before/after IOS state, execution read-back, and secret-log checks are unavailable.
- Added `docs/lab/cisco-ios-gns3.md` and `scripts/cisco-ios-preflight.sh`. The preflight requires an independently verified fingerprint and SSH public-key auth, then runs only `show version`. Syntax and invalid-input paths were checked locally; no target was contacted.
- `bash -n scripts/cisco-ios-preflight.sh` exited 0. No-argument, invalid-host, and invalid-fingerprint paths each exited 64 before network access.
- `go test -count=1 ./provider/internal/adapters/executors ./provider/internal/delivery/httpapi`, `go test -count=1 ./...`, `make fmt-check`, `make vet`, and `make build` each exited 0. Automated tests are not live lab evidence.
- No executor defect was confirmed by a real lab; no executor implementation/test was changed. No Bloc 4 work was performed.