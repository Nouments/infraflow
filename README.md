# InfraFlow

InfraFlow is a declarative infrastructure orchestration project. Its design follows `INFRAFLOW_SPEC.md`: generic orchestration core, explicit adapter capabilities, and a strict separation between validation, planning, and execution.

## Current scope

This repository starts with the foundation and first safe CLI workflow:

```text
infra.yaml -> validate -> plan -> generate deterministic artifacts
```

This workflow does not configure devices or execute external provisioning tools. Provider-specific provisioning remains unsupported until it has verified references, an isolated adapter, fixtures or a lab, automated tests, and observable results.

## Requirements

- Go 1.23 or newer

## Quick start

```sh
go test ./...
go run ./cmd/infraflow validate -f examples/infra.yaml
go run ./cmd/infraflow plan -f examples/infra.yaml
go run ./cmd/infraflow generate -f examples/infra.yaml -out ./generated
```

`validate` is read-only. `plan` only describes deterministic local work. `generate` writes artifacts only to the requested output directory; it does not contact infrastructure.

## Project layout

```text
cmd/infraflow/       CLI entry point
internal/domain/     Core infrastructure and plan types
internal/config/     YAML loading and validation
internal/planner/    Deterministic dependency planning
internal/generator/  Local, deterministic artifact generation
examples/            Sample infrastructure declarations
docs/                Architecture and implementation notes
```

## Development

```sh
gofmt -w ./cmd ./internal
go vet ./...
go test -race ./...
go build ./cmd/infraflow
```

## Implementation status

- [x] Go module and initial project structure
- [x] README, architecture notes, and example input
- [ ] Strict YAML parsing and semantic validation
- [ ] Dependency plan generation
- [ ] Deterministic inventory and topology artifacts
- [ ] Agent, backend, services, provider adapters, and web UI

The later items are intentionally not represented as supported capabilities yet. See `INFRAFLOW_SPEC.md` for the full phased roadmap and acceptance criteria.