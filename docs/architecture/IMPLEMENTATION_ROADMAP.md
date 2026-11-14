# Incremental Implementation Roadmap

## Active generation-engine follow-up

**Starting reference:** `develop` at
`23f3325ad24651d506f96d962f1b5de25c03ebbc` (`stabilization4`), fetched and
confirmed equal to `origin/develop`; initial worktree clean. M1 changes are
local and uncommitted.

M1 implements coherent `ArtifactRepository.Open` reads by reusing the
lock-protected, hash-verifying `ReadArtifact` path. It returns a bounded byte
snapshot; concurrent catalog scans also ignore transient reserved lock files.
A streaming descriptor was rejected because Windows can prevent atomic
replacement while the file remains open. Focused tests passed, including ten
repeated concurrent-reader runs. The latest full local suite is blocked by
Windows Application Control on the temporary Provider executable in the
multisite test and `CGO_ENABLED=0` SQLite runtime failures. Local race testing
is unavailable without CGO.

The latest CI run on the starting commit,
[43624108997](https://github.com/Nouments/infraflow/actions/runs/43624108997),
passed all Linux and Windows steps. It predates M1 changes; post-change CI is
unverified because no push is authorized.

| Milestone | Scope | Status |
|---|---|---|
| M1 | Reproduce and fix `ArtifactRepository.Open` consistency; cover generation changes, deletion, readers, and writer contention. | Implemented locally; targeted tests pass. |
| M2 | Audit and strengthen transactional publication; inject failures by publication phase. | Not started. |
| M3 | Implement conservative crash recovery or explicit manual-recovery state. | Not started. |
| M4 | Harden and document the public reader/writer locking contract, including cancellation and stale-lock behavior. | Not started. |
| M5 | Harmonize declarative, standalone, site, and catalog artifact contracts with compatible metadata. | Not started. |
| M6 | Harden filesystem operations and document portable threat-model limits. | Not started. |
| M7 | Verify deterministic output across implemented Cisco, MikroTik, and Fortinet paths. | Not started. |
| M8 | Expand independent generation, manifest, integrity, concurrency, recovery, and security tests. | M1-specific tests added; broader milestone not started. |
| M9 | Verify Linux and Windows CI on the changed revision. | Starting-reference CI is green; M1 revision still needs CI. |

This mission is limited to the generation engine. Developer/Reviewer agents,
orchestration, and unrelated vendor capabilities are explicitly out of scope.

## Prior broad architecture roadmap (separate from this mission)

> The following was written for an earlier multi-phase architecture planning
> task. Its coding-agent and sandbox milestones are not part of the active
> generation-engine follow-up above.

### Historical baseline and principles

That earlier roadmap was based on the code and executed evidence at
`develop` HEAD `9f60e0e51104e59875a5156bc13c9ac31c388baf`. The initial worktree
was clean. Phase 0 audits are in this directory.

The latest observed GitHub Actions run on that commit passed all Linux checks.
Windows failed before checkout, so it provides no test evidence. The workflow
failure has been corrected locally by removing the pre-checkout Git config
command; the correction still needs a subsequent CI run. The audit machine's
full suite was blocked by Windows Application Control and CGO-disabled SQLite.

Guiding constraints:

- Stabilize generation and its artifact contract before introducing
  model-driven code changes.
- Keep generated files declarative and inert; do not add automatic device
  configuration as part of this roadmap.
- No Ansible and no unverified vendor capabilities.
- Do not call temp directories, prompt instructions, or mock tests a sandbox.
- Never allow a Developer Agent to approve its own changes.
- Preserve explicit human approval for changes outside an isolated workspace.

### Historical M0 — Establish the evidence baseline (complete)

**Outcome:** four evidence-based architecture/audit/roadmap documents, the
actual Linux/Windows CI result recorded, and the pre-checkout Windows workflow
failure corrected in source.

**Acceptance:** branch/HEAD and worktree state recorded; actual workflow run
identified; local failures reported without masking environmental causes;
no hardware, test, or runtime claims inferred from documentation alone.

### Historical M1 — Close generation consistency gaps (highest priority)

**Objective:** make all supported generation entry points preserve one
consistent and inspectable output state.

1. Define a durable, versioned publication journal and recovery behavior for
   interrupted full-generation publication. Validate journal paths, hashes,
   file types, and root containment before recovery. Invalid recovery metadata
   must fail closed without modifying existing user files or the last valid
   generation.
2. Fix the Provider artifact catalog/open race by reading from a coherent,
   hash-verified snapshot under the same coordination used by generation.
3. Specify standalone generator behavior: either route it through a shared
   transaction/catalog contract or explicitly maintain a separate documented
   output contract. Do not imply full-generation rollback where it is absent.
4. Align declarative `device_configuration` outputs with a supported artifact
   manifest/path contract, or explicitly define and test a separate contract.
5. Preserve unrelated files and directories; remove only prior catalog-owned
   obsolete artifacts. Keep site manifests, global manifest, and reports
   consistent on success and failure.

**Required regression evidence:** failures at each publication phase, recovery
after simulated interruption, malformed recovery data, stale-service and
removed-device cleanup, unrelated-file preservation, idempotency, hashes,
concurrent generation/readers, cancellation, traversal, and symlinks.

**Exit criteria:** all focused generation/filesystem/protocol tests pass on
Linux and Windows; failed publication preserves the previous valid state;
successful catalog entries exactly match existing bytes and hashes; no
unowned files are removed or cataloged.

### Historical M2 — Expand end-to-end reliability evidence

**Objective:** turn the supported generation contract into a repeatable
cross-platform verification path.

- Use representative existing fixtures and fresh output roots.
- Exercise `validate`, `plan`, and `generate-all`; verify site isolation,
  manifest/report agreement, size/hash integrity, and unsupported-intent
  rejection.
- Keep fixtures deterministic and avoid claims based only on mocks.
- Add bounded fuzz/property checks only where parsers, path contracts, or
  manifest invariants benefit from them.
- Capture baseline and benchmark generation only after correctness assertions
  exist; performance thresholds must be measured and reproducible.

**Exit criteria:** focused and full test suites pass on the supported Linux and
Windows CI workers, with the exact command and toolchain recorded.

### Historical M3 — Specify and build an enforceable execution boundary

**Objective:** establish an isolated, auditable workspace and command runner
before enabling any write-capable coding worker.

- Select and document platform-specific OS isolation for Linux and Windows;
  define CPU, memory, process, filesystem, network, time, and output limits.
- Restrict executable selection to an explicit allowlist and controlled
  environment; do not inherit ambient credentials or `PATH` unchecked.
- Separate read-only inspection from write-capable operations.
- Define cleanup, cancellation, crash recovery, audit logging, and secret
  handling.
- Threat-model hostile repository contents and tool output.

**Gate:** if equivalent enforceable isolation cannot be provided on both
supported platforms, keep code-writing execution disabled rather than
substituting prompt rules or a temp directory.

### Historical M4 — Add an independent Reviewer role

**Objective:** produce structured, read-only findings for a task diff and its
actual validation results.

- Use a provider-neutral model/runtime interface only after its dependency,
  data handling, and failure modes are approved.
- Give the reviewer read-only access to the isolated task workspace.
- Record findings with file/line, severity, evidence, and confidence.
- Require the reviewer to inspect test results rather than trust generated
  summaries. Make review unavailable when the implementation and reviewer
  share the same write authority or state.

**Exit criteria:** malformed/empty model responses fail explicitly; reviewer
cannot modify the workspace; independent test cases cover unsafe or
success-shaped outputs.

### Historical M5 — Add a bounded Developer role

**Objective:** implement narrowly scoped tasks in an isolated worktree.

- Require an approved task description, explicit file/scope bounds, and a
  validated starting revision.
- Use only the sandbox command interface from M3; no arbitrary host shell,
  device credentials, or deployment network access.
- Persist patch, tool output, and validation status; never self-approve,
  commit, or push.
- Stop on scope expansion, repeated failure, cancellation, or resource limit.

**Exit criteria:** all writes remain in the task workspace; host repository and
external systems are unchanged absent explicit approval.

### Historical M6 — Add the task Orchestrator

**Objective:** coordinate Developer, Reviewer, and human approval with durable,
versioned state.

- Persist tasks, transitions, attempts, cancellation, and audit data.
- Enforce legal state transitions and bounded correction cycles.
- Require successful validation and independent review before requesting
  human approval; no automated merge or push.
- Recover safely after restart and prevent duplicate or conflicting writes.

**Exit criteria:** state-machine, concurrency, restart, cancellation,
authorization, and approval-bypass tests pass.

### Historical M7 — CI and release evidence

**Objective:** make CI exercise the full workflow and produce observed,
reviewable results.

- Keep Linux and Windows jobs on supported shells and actual supported Go
  versions; avoid configuration before checkout that requires a repository.
- Run full tests, race tests where supported, vet, build, formatting,
  diff checks, generation integrity, and sandbox boundary tests.
- Test workflow changes using actual Actions runs; workflow YAML alone is not
  success evidence.
- Record missing toolchains or Application Control restrictions as
  environment limitations, not passing tests.

**Exit criteria:** both required platform runs are green on the final revision;
artifacts/logs identify commit SHA and exact commands.

### Historical M8 — Documentation and handoff

**Objective:** keep README, status reports, architecture, and release procedure
aligned with implementation and evidence.

- State supported/partial/missing behavior and platform limitations.
- Separate generation, transfer, execution, and verification.
- Include reproducible commands, actual test/CI outcomes, and missing lab
  evidence.
- Keep historical records clearly labeled; do not let stale status statements
  appear current.

### Historical definition of done

The system is not ready to enable autonomous code changes until M1-M7 exit
criteria pass, both platform CI runs succeed, the sandbox threat model is
validated, and human approval remains an enforced boundary. No vendor should
be called hardware-validated without actual evidence.
