# Architecture

## Initial boundary

The first implementation is a local CLI with a one-way flow:

```text
YAML input -> typed model -> side-effect-free validation -> plan -> local artifacts
```

The CLI delegates to internal packages. Domain types do not depend on YAML, HTTP, a database, Ansible, Terraform, SSH, or a device vendor. The configuration package owns YAML decoding and input diagnostics; the planner consumes only validated domain data; generators produce deterministic, local files.

## Safety boundary

- Validation never writes files or contacts infrastructure.
- Planning describes work; it does not execute it.
- Artifact generation is confined to the explicitly selected output directory.
- No device provisioning adapter is enabled in this initial slice.
- Unknown provider capabilities remain unknown and are never treated as supported.

## Growth path

Add ports and application use cases around the validated domain and planner before adding persistence, an agent, or a backend. Provider-specific behavior belongs in isolated adapters and must satisfy the support evidence required by `INFRAFLOW_SPEC.md`.