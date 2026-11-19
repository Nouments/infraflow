# Architecture review — 2026-11-19

Scope: the parts exercised against real devices today. The layered design
(domain → application → ports → adapters; Provider and Agent as separate
binaries sharing only `pkg/protocol` and the protobuf contract) was preserved;
no component was replaced. A fuller earlier review is in
[ARCHITECTURE_REVIEW.md](../../docs/DELIVERY/ARCHITECTURE_REVIEW.md).

## Observations

1. **Profile selection is exact and table-free in places.** Cisco IOS
   AutoInstall was hard-coded to the 7200 in three files (domain profile,
   native profile resolution, method validation). It is now a single table
   (`domain.CiscoIOSAutoInstallProfileFor`) carrying model aliases, release,
   template ID, capability state and evidence. Adding a model is one entry plus
   a versioned template directory; no fallback between releases exists.
2. **Metadata semantics drifted from the matcher.** Templates declared `7.x`,
   the domain layer interpreted it as a train (`HasPrefix("7.")`), and the
   generator compared it literally. The train rule now lives in one function
   (`matchesReleaseTrain`) with explicit, tested bounds.
3. **Two log formats, one consumer.** Agent bootstrap services emit structured
   JSONL when started with `-agent-config` and plain lines otherwise; the
   diagnostics reader only understood the former. It now parses the plain
   format's `<time> <level> <event> <detail>` and the `reservation <device>`
   correlation hint, without inventing identities for lines that lack one.
4. **Error propagation across the application boundary.** The execution
   service replaced executor validation messages with a fixed string. Executor
   messages describe operator input only, so they are now wrapped, not
   replaced.
5. **Trust material lifecycle.** `known_hosts` was a startup snapshot. It is now
   read per verification (cost: one small file read per SSH connection).

## Gaps that remain architectural

- No InfraFlow delivery adapter for RouterOS (FTP/SFTP upload, `.auto.log`
  retrieval) or FortiOS (API/SSH); both vendors stop at rendering.
- Device-console evidence enters diagnostics only through files supplied by an
  operator or harness; there is no InfraFlow console collector.
- SSH algorithm policy is global (x/crypto defaults); there is no per-profile
  minimum.
- The GNS3 harness (`scripts/lab/`) is external tooling; the integration
  scenarios are not yet a repeatable automated suite.
