# Architecture review

## Product boundary

InfraFlow consists of two separate programs:

- **Provider:** validates and plans declared infrastructure, generates and
  publishes artifacts, serves the authenticated catalog, and stores Agent
  reports.
- **Agent:** registers with the Provider, downloads site-authorized artifacts,
  checks hashes, stages supported state, and reports processing results.

They communicate through the existing versioned gRPC and HTTP contracts. The
Agent's successful staging of native configuration is not device execution.
Bootstrap services remain opt-in and are not started by this test.

GNS3 is outside the product boundary. The local GNS3 controller API was used
as test setup and observation tooling to create a new project, add the two
existing Docker appliance types and an isolated switch, connect them, and
collect results. No GNS3-specific product source or runtime dependency was
added.

## Change reviewed

The Agent filesystem adapter accepted several artifact types but omitted
`native_device_config` and `native_manifest` from the set of valid stored
artifact path types. The generic processor already treated these as verified,
non-executing artifacts. That mismatch caused the first real appliance run to
reject the two files after transfer.

The change adds these two types to the existing path allowlist and adds tests
for saving, reopening, and staging them. It does not change the wire protocol,
Provider behavior, execution permissions, or device-side behavior.

## Trust and failure boundaries

- Provider-to-Agent transfer is authenticated and TLS protected in the tested
  topology. The Agent verifies artifact hashes before local storage.
- Site-scoped authorization remains the boundary for which catalog an Agent
  can fetch and report against.
- Native configuration, Terraform, and bootstrap files are staged data. The
  normal Agent processing path does not execute them.
- The GNS3 lab had no Cloud or NAT node and no connection to an external
  network. Only the new project and the two new delivery templates were used.
- GNS3 itself was configured for HTTP on a wildcard listen address and its
  supplied log contained an exposed bearer token. See
  [`SECURITY_AUDIT.md`](SECURITY_AUDIT.md); this is a lab-host security issue,
  not a new InfraFlow code path.

## Review conclusion

The native-artifact allowlist fix is narrowly scoped and has regression
coverage. The live test confirms the real Provider and Agent binaries can
complete the existing authenticated synchronization flow and persist its
report. Device execution and vendor compatibility remain separate, unproven
capabilities.
