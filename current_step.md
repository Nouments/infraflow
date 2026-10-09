# InfraFlow — Current Engineering Tasks

## Objective

Correct the documentation and complete the technical observability workflow across the Provider Web Dashboard, Agent, and TUI.

Work on the current feature branch. Do not push directly to `main` or `develop`. Preserve existing architecture and working behavior. Avoid unrelated refactoring.

## Mandatory engineering rules

* Do not invent test results, device states, execution results, logs, or capabilities.
* Do not use mock data in production views. If mock data is used in tests, identify it explicitly.
* Do not mark a task complete without implementation evidence and test results.
* Preserve authentication, authorization, input validation, and existing secret redaction.
* Keep stdout and stderr separate.
* Do not log passwords, tokens, private keys, credentials, or sensitive configuration values.
* Do not execute real network changes as part of tests.
* Never report a generated plan or artifact as an executed deployment.
* Document limitations and blocked tasks explicitly.

## P0 — Documentation integrity

* [ ] Inspect all README links and verify each target exists.
* [ ] Resolve the broken `INFRAFLOW_SPEC.md` reference: restore the authoritative specification if available; otherwise update the README to identify the missing source and document the currently verified scope without fabricating requirements.
* [ ] Create and maintain this `CURRENT.md`.
* [ ] Verify branch differences before merging or copying changes between branches.
* [ ] Update documentation only with behavior verified in the source code and tests.

## P1 — Technical logs in the Web Dashboard

* [ ] Inspect existing log models, storage, filtering, authorization, and API handlers.
* [ ] Add a dedicated Technical Logs view to the Web Dashboard.
* [ ] Integrate the existing `GET /api/v1/logs` endpoint.
* [ ] Integrate `GET /api/v1/logs/runs/{id}` where appropriate.
* [ ] Integrate `GET /api/v1/logs/stream` using its actual protocol and response format.
* [ ] Display timestamp, severity, component, event, run/job ID, agent/site identifiers when available, and message.
* [ ] Support filtering and error states only where the backend supports them.
* [ ] Provide loading, empty, disconnected, unauthorized, and server-error states.
* [ ] Do not treat the Audit Trail as equivalent to technical logs.
* [ ] Do not fabricate records when the API returns no data.

## P1 — Real-time process output

* [ ] Inspect the existing toolrunner and job execution lifecycle.
* [ ] Implement bounded, incremental stdout and stderr capture while a process is running.
* [ ] Forward output through the existing logging/event infrastructure where supported.
* [ ] Associate events with the actual run/job ID and component.
* [ ] Preserve output size limits, cancellation, timeouts, and secret redaction.
* [ ] Avoid unbounded goroutines, channels, buffers, and database writes.
* [ ] Define behavior for slow or disconnected log consumers.
* [ ] Ensure final process results still include exit status, duration, truncation information, and separately captured stdout/stderr.
* [ ] Do not claim live streaming if events are emitted only after process completion.

## P1 — TUI technical logs

* [ ] Inspect the current TUI architecture and navigation before modifying it.
* [ ] Add a technical log view only if it fits the existing architecture.
* [ ] Display actual backend or agent log events, not fabricated entries.
* [ ] Provide refresh, loading, empty, and error states as supported.
* [ ] Preserve keyboard navigation and existing TUI behavior.
* [ ] Clearly distinguish historical logs from a live stream.

## P1 — Tests and security

* [ ] Add unit tests for incremental output capture.
* [ ] Test stdout/stderr separation.
* [ ] Test output truncation, process failure, cancellation, and timeout.
* [ ] Test secret redaction before logs leave the process boundary.
* [ ] Add API tests for authorization and log retrieval.
* [ ] Add frontend tests for loading, empty, error, and real API data.
* [ ] Test stream disconnect and reconnect behavior where applicable.
* [ ] Test that non-admin users cannot access admin-only log endpoints.
* [ ] Run the repository's documented tests and report actual results.
* [ ] Do not claim integration, end-to-end, or lab tests were run unless they were actually executed.

## P2 — README accuracy

* [ ] Document the actual logs architecture and API endpoints.
* [ ] Explain the difference between Audit Trail and technical logs.
* [ ] Document whether process output is buffered or streamed in real time.
* [ ] Update feature checklists only after implementation and tests.
* [ ] Identify mock, experimental, generated-only, unverified, and lab-tested functionality accurately.
* [ ] Verify every documentation link.

## Definition of Done

A task is complete only when:

1. The implementation exists.
2. Relevant automated tests pass.
3. Security and authorization behavior is tested where applicable.
4. Documentation matches the implementation.
5. Limitations are recorded.
6. The final report lists changed files, commands actually executed, test outcomes, and remaining blockers.

## Final report

Provide:

* Summary of changes.
* Files changed.
* Tests executed and their actual outcomes.
* Features implemented but not lab-tested.
* Remaining TODOs and blockers.
* Confirmation that no real network device configuration was changed without explicit authorization.
