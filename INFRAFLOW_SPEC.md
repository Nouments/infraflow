# InfraFlow Specification

This document records the repository's current contract and roadmap. It is intentionally explicit about what is implemented, what is under active design, and what remains planned or blocked.

## 1. Scope and principles

InfraFlow is a declarative infrastructure orchestration project. It separates the platform into three concerns:

- provider-side validation, planning, artifact generation, and authenticated APIs;
- agent-side catalog download, local state verification, and supported local processing;
- observability and audit data that remain local-first and can be synchronized upward when the backend is reachable.

The project follows a strict evidence-based model:

- generated configuration is not treated as deployed state;
- no vendor-specific provisioning is considered supported unless an adapter and evidence are present;
- future features remain future features until they are implemented and tested.

## 2. Implemented behavior

### Provider

The provider currently implements:

- YAML validation and semantic checks for infrastructure inputs;
- deterministic planning and job persistence;
- a separate persistent execution lifecycle connected to the existing scheduler, with task attempts, results, cancellation, recovery, and audit events;
- generation of hash-verified Ansible, Terraform, and bootstrap artifact sets;
- authenticated gRPC catalog and report services;
- authenticated HTTP API for user sessions, planning jobs, admin-only execution tasks, agents, and audit events;
- local SQLite-backed user accounts, session management, and RBAC.

The generic execution API is available at `/api/v1/executions`. The Provider
registers two operational task executors: `artifact.verify` (read-only
cryptographic SHA-256 integrity verification of catalog artifacts) and
`cisco.ios.configure` (verified Cisco IOS hostname and Ethernet interface
configuration via SSH with running-config read-back verification).
Unregistered or unsupported task types remain rejected. The execution lifecycle,
attempts, results, and audit persistence are operational; network configuration
deployment for other platforms (FortiGate, MikroTik, Proxmox) and advanced network
features (NAT, ACL, dynamic routing) remain unimplemented.

### Agent

The agent currently implements:

- authenticated provider registration and heartbeat reporting;
- hash-verified artifact download and local state storage;
- processing of generated generic Ansible and Terraform artifacts in read-only validation mode;
- local structured logging and delayed synchronization to the provider when available.

### Observability

The repository includes:

- JSONL event logging with size limits and file rotation;
- structured event normalization and redaction of password-like fields;
- central log storage, query filters, and SSE-compatible streaming endpoints;
- local outbox handling for delayed log synchronization.
- an admin-only dashboard technical-log view and an admin-only point-in-time TUI
	query using the existing HTTP log contract;
- admin-only execution list, detail, and cancellation views in both the web dashboard
	and the Linux TUI;
- bounded incremental process stdout/stderr capture, with optional local outbox
	persistence and synchronization over the existing agent gRPC log reporter.

## 3. Explicitly planned or blocked behavior

The following items are not treated as operationally supported by the current codebase:

- operational live GNS3 / lab validation of the Cisco IOS executor against physical/virtual hardware (tracked as NON EXÉCUTÉ);
- vendor-specific configuration executors for FortiGate, MikroTik, Proxmox, or other platforms;
- advanced Cisco IOS features (NAT, ACL, dynamic routing, switchport / VLANs);
- automatic startup-config saving (`write memory`), pending a separate explicit save task;
- remote network automation beyond controlled lab validation and repository tests;
- bootstrap services that are not explicitly implemented and tested.

These remain in the roadmap and are tracked as planned, partial, or blocked depending on evidence.

## 4. Evidence requirements

Before a feature is marked as implemented, it must satisfy the repository test and documentation bar:

- tests must execute against real behavior rather than mock-only assertions;
- commands and outputs must be observed, not inferred;
- local and central log paths must be checked against the current runtime code;
- any lab or network verification must be clearly labeled as such.

## 5. Repository conventions

The current implementation uses the following conventions:

- provider and agent are separate services under their own top-level directories;
- data and APIs remain explicit and versioned;
- generated files are not treated as execution evidence;
- docs must describe the current state accurately rather than the desired state.

This specification should be read as the repository's working boundary: it defines the supported surface and the future work that still must earn proof before it is promoted to an implemented capability.
