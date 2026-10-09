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
- generation of hash-verified Ansible, Terraform, and bootstrap artifact sets;
- authenticated gRPC catalog and report services;
- authenticated HTTP API for user sessions, jobs, agents, and audit events;
- local SQLite-backed user accounts, session management, and RBAC.

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

## 3. Explicitly planned or blocked behavior

The following items are not treated as operationally supported by the current codebase:

- real device provisioning adapters;
- execution job submission through the provider API;
- vendor-specific configuration execution for Cisco, FortiGate, MikroTik, Proxmox, or other platforms;
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
