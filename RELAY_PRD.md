# Relay — Product Requirements Document (PRD)

**Document status:** Draft v0.1  
**Product:** Relay  
**Product type:** Self-hosted background job processing and orchestration platform  
**Primary audience:** Developers and engineering teams  
**Language:** English

---

## 1. Product Overview

Relay is a self-hosted background job processing and orchestration system. It allows applications to submit work that should be performed asynchronously, then provides the infrastructure to persist, schedule, execute, retry, monitor, and manage those jobs.

Relay separates the request lifecycle of an application from potentially long-running, unreliable, or resource-intensive work.

The initial release will prioritize a reliable core built on PostgreSQL, with a local-first deployment model and no mandatory external cloud services.

## 2. Product Vision

Make background processing observable, reliable, and approachable for developers by providing a simple API, a resilient worker engine, and a clear operational dashboard.

## 3. Problem Statement

Applications often need to perform work that should not happen during a synchronous request, such as report generation, imports, notifications, or integrations with external services.

Implementing this independently in every application leads to duplicated infrastructure and inconsistent handling of:
- Persistence and job state.
- Concurrent processing.
- Retries and failure handling.
- Scheduling and prioritization.
- Worker health and recovery.
- Operational visibility.

Relay provides a reusable system for these concerns.

## 4. Goals

### 4.1 MVP goals

- Submit jobs through a REST API.
- Persist jobs durably in PostgreSQL.
- Execute jobs through configurable worker pools.
- Support multiple job types and queues.
- Provide a defined job lifecycle.
- Handle transient failures with configurable retries and backoff.
- Support job execution timeouts and cancellation where feasible.
- Recover jobs when workers disappear or leases expire.
- Support delayed/scheduled jobs and priority ordering.
- Provide an operational web dashboard.
- Expose metrics and structured logs.
- Run entirely locally using Docker Compose.

### 4.2 Product principles

1. **Local-first:** The complete MVP runs on a developer's machine.
2. **Reliability over feature count:** Correct lifecycle and recovery behavior takes priority over breadth.
3. **Observable by default:** Job state and execution history should be easy to inspect.
4. **Developer-friendly:** API and configuration should be approachable and documented.
5. **Explicit behavior:** State transitions, retries, timeouts, and cancellation rules should be documented.
6. **Incremental complexity:** Start with PostgreSQL; introduce additional infrastructure only when justified.

## 5. Non-Goals

The MVP will not attempt to:
- Guarantee exactly-once execution of arbitrary external side effects.
- Replace a general-purpose workflow engine for complex multi-step orchestration.
- Execute arbitrary shell commands or user-uploaded code.
- Provide a hosted SaaS offering.
- Require Kubernetes or cloud infrastructure.
- Include multi-tenant SaaS billing or enterprise administration.
- Provide a built-in email, PDF, or import implementation beyond illustrative example handlers.
- Implement distributed workflow graphs, DAGs, or job dependencies in the MVP.
- Support every language through SDKs in the first release.

## 6. Target Users

### Primary persona: Application developer

A developer integrating asynchronous processing into an application. They want to submit jobs, inspect status, configure retry behavior, and understand failures without building their own job infrastructure.

### Secondary persona: Developer/operator

A developer responsible for running Relay. They need to monitor queue health, worker activity, processing throughput, failures, and operational issues.

## 7. Core Concepts

### 7.1 Job

A persisted unit of work submitted for asynchronous execution.

A job includes, at minimum:
- Unique ID.
- Job type.
- Queue.
- Payload.
- Status.
- Priority.
- Attempt count and maximum attempts.
- Created, scheduled, started, and completed timestamps as applicable.
- Execution timeout.
- Last error / failure information.
- Worker assignment and lease information while processing.

### 7.2 Job type / handler

A job type identifies the application-defined handler that Relay invokes to process the job. Relay manages execution lifecycle; the handler implements task-specific business logic.

### 7.3 Queue

A logical grouping of jobs. Queues allow workloads to be separated and workers to consume selected queues.

### 7.4 Worker

A running processing instance that claims jobs and executes handlers. Workers have an identity, configured queue subscriptions, concurrency, and health/heartbeat information.

### 7.5 Attempt

One execution attempt of a job. Attempts should be inspectable, including timing and error information.

### 7.6 Scheduled job

A job that becomes eligible for processing at or after a specified time.

## 8. Job Lifecycle

### 8.1 MVP statuses

- **Pending:** Eligible for processing when scheduling constraints permit.
- **Running:** Claimed by a worker and being processed.
- **Completed:** Successfully processed.
- **Failed:** Exhausted its retry policy or otherwise failed permanently.
- **Cancelled:** Cancelled before completion, subject to the cancellation rules.

A scheduled job may remain Pending while its scheduled time is in the future; scheduling is represented by a timestamp rather than necessarily a distinct status.

### 8.2 Lifecycle behavior

- Jobs are created in Pending state.
- A worker atomically claims an eligible job and transitions it to Running.
- Successful handler completion transitions the job to Completed.
- A retryable failure schedules another attempt if the retry policy allows it.
- If no retries remain, the job transitions to Failed.
- Cancellation is allowed only in states and circumstances defined by the implementation; completed jobs cannot be cancelled.
- State transitions must be validated and persisted consistently.

### 8.3 Delivery semantics

Relay aims for **at-least-once execution**, not exactly-once execution. A job may be executed more than once, especially when a worker fails after performing a side effect but before completion is persisted.

Handlers should be designed to be idempotent where possible. Relay may provide idempotency-related facilities in a later iteration, but the MVP must clearly document these semantics.

## 9. Functional Requirements

### FR-01 — Job submission

The system must allow jobs to be created through a REST API with a job type, payload, queue, priority, retry configuration, and optional schedule/timeout settings.

### FR-02 — Durable persistence

Submitted jobs must be persisted in PostgreSQL and survive Relay restarts.

### FR-03 — Job retrieval and listing

Users must be able to retrieve a job by ID and list jobs with filtering and pagination.

### FR-04 — Job execution

Workers must invoke the registered handler for the job type and record the result of the execution attempt.

### FR-05 — Concurrency-safe claiming

Multiple workers must be able to claim jobs concurrently without intentionally claiming the same job simultaneously. Claiming and state changes must use atomic database operations.

### FR-06 — Configurable worker concurrency

Workers must support configurable concurrency and queue subscriptions.

### FR-07 — Retry policy

Jobs must support a configurable maximum number of attempts and a retry delay policy. The MVP should implement exponential backoff, with jitter considered as part of the implementation.

### FR-08 — Execution timeout

Jobs must support a configurable execution timeout. Timeout behavior and limitations of handler cancellation must be documented.

### FR-09 — Worker recovery

The system must detect jobs whose worker lease has expired and recover them according to documented retry and attempt-count rules.

### FR-10 — Scheduling

Jobs must support execution at or after a specified scheduled time.

### FR-11 — Priority

Jobs must support priority-based ordering within the queue selection policy. The ordering policy and starvation limitations must be documented.

### FR-12 — Queue management

Users must be able to inspect queues and their backlog. Workers may subscribe to one or more queues.

### FR-13 — Manual retry

Users must be able to manually retry eligible failed jobs through the API and dashboard. Manual retry behavior must be explicit regarding attempts and history.

### FR-14 — Cancellation

Users must be able to request cancellation of eligible jobs. Cancellation behavior for already-running handlers must be documented and must not imply that arbitrary external side effects can be undone.

### FR-15 — Job logs and attempt history

The system must expose job execution attempts, timing, and failure details. Structured application logs should be correlated with job and worker IDs.

### FR-16 — Worker visibility

The dashboard and API must expose worker identity, health/heartbeat, queue subscriptions, concurrency, and active jobs where available.

### FR-17 — Metrics

Relay must expose metrics suitable for Prometheus collection, including job counts, execution duration, queue wait time, retries, queue depth, and worker-related metrics.

### FR-18 — Health endpoints

Relay must provide health information for its API and relevant dependencies.

### FR-19 — Configuration

Relay must support documented configuration for database connection, worker concurrency, queue subscriptions, retry defaults, timeouts, and telemetry.

### FR-20 — Local deployment

The complete MVP must be runnable locally using Docker Compose and documented setup instructions.

## 10. API Requirements (Product-Level)

The API is REST-oriented and versioned.

Initial conceptual endpoints:
- `POST /api/v1/jobs` — submit a job.
- `GET /api/v1/jobs/{id}` — retrieve job details.
- `GET /api/v1/jobs` — list/search/filter jobs.
- `POST /api/v1/jobs/{id}/retry` — request manual retry.
- `POST /api/v1/jobs/{id}/cancel` — request cancellation.
- `GET /api/v1/queues` — inspect queues.
- `GET /api/v1/workers` — inspect workers.
- `GET /health` — health check.
- `GET /metrics` — metrics endpoint (implementation details may depend on deployment).

Detailed schemas, validation rules, error responses, and endpoint design belong in the Technical Design document.

## 11. Dashboard Information Architecture

The web dashboard will include the following areas:

### 11.1 Overview

Purpose: quickly understand the system's operational state.

Include:
- Summary metrics: Pending, Running, Completed, Failed.
- Job throughput over time.
- Queue backlog and queue latency.
- Worker health / utilization.
- Recent failures.
- Recent activity.

### 11.2 Jobs

Purpose: find and manage individual jobs.

Include:
- Table/list of jobs.
- Search by ID.
- Filters for status, queue, type, time range, and worker where applicable.
- Pagination.
- Clear status indicators.
- Access to job details.

### 11.3 Job detail

Purpose: understand the lifecycle and diagnose a single job.

Include:
- Job identity and type.
- Status and key timestamps.
- Payload view (formatted JSON).
- Attempt history / timeline.
- Error and failure details.
- Logs correlated to the job.
- Worker assignment.
- Retry and cancellation actions when eligible.

### 11.4 Queues

Purpose: understand workload distribution.

Include:
- Queue names.
- Pending backlog.
- Running jobs.
- Failed counts where useful.
- Queue-specific throughput or latency.
- Worker subscriptions where relevant.

### 11.5 Workers

Purpose: inspect processing capacity and worker health.

Include:
- Worker identity.
- Health / last heartbeat.
- Queues consumed.
- Concurrency.
- Active jobs.
- Uptime or last-seen information.

### 11.6 Metrics / observability

Purpose: examine system performance and behavior.

Include:
- Job throughput.
- Execution duration.
- Queue latency.
- Retry/failure trends.
- Worker capacity and activity.

The dashboard may embed or link to Grafana during the MVP; it does not need to recreate a full metrics platform.

## 12. UX and Visual Design Requirements

### 12.1 Design direction

Relay should feel like a modern developer tool: dark-first, information-dense without feeling cluttered, restrained, precise, and responsive.

Reference qualities:
- Dark surfaces and clear hierarchy.
- Neutral base colors.
- Teal/cyan as a restrained accent.
- Semantic colors for status and alerts.
- Strong typography and tabular numeric alignment.
- Compact tables with comfortable readability.
- Subtle borders and dividers rather than excessive card decoration.
- Clear focus states and keyboard navigation.
- Consistent spacing, sizing, and interaction patterns.

### 12.2 Layout

- Persistent left navigation rail/sidebar.
- Top bar with product identity, environment/system status, and relevant controls.
- Main content area with consistent page gutters.
- Page titles and concise contextual descriptions.
- Responsive behavior for smaller viewports.
- Avoid excessive nested cards and large decorative elements.

### 12.3 Status colors

Use status colors consistently:
- Pending: neutral/blue.
- Running: teal/cyan.
- Completed: green.
- Failed: red.
- Cancelled: muted neutral.

Status should never be communicated by color alone; include text or icons.

### 12.4 Dashboard interactions

- Tables should support sorting where meaningful, filtering, pagination, and clear empty/loading/error states.
- Job details should make the lifecycle and failure information easy to scan.
- Destructive or irreversible actions should require confirmation where appropriate.
- Retry and cancel controls should only appear when applicable.
- JSON payloads and error details should be readable and copyable.
- Time and duration formats should be consistent.

### 12.5 Accessibility

- Keyboard navigability.
- Visible focus indicators.
- Adequate contrast.
- Semantic labels and headings.
- Status information accessible without color alone.
- Respect reduced-motion preferences.

## 13. MVP Acceptance Criteria

The MVP is considered functionally complete when:

1. A developer can run Relay and its dependencies locally using the documented Docker Compose workflow.
2. A job can be submitted via API and persisted.
3. A worker can claim and process a registered job type.
4. Jobs are not simultaneously claimed by multiple workers under normal concurrent claiming conditions.
5. Successful jobs become Completed.
6. Retryable failures follow the configured retry policy.
7. Jobs that exhaust retries become Failed.
8. Scheduled jobs are not claimed before their scheduled time.
9. Worker failure and lease-expiry recovery are demonstrated and documented.
10. Job listing and detail views are usable in the dashboard.
11. Users can inspect attempts and failures.
12. Users can manually retry eligible failed jobs and cancel eligible jobs.
13. Queue and worker information is available.
14. Metrics can be scraped by Prometheus.
15. The dashboard has clear loading, empty, error, and responsive states.
16. The README documents setup, usage, configuration, and known limitations.

## 14. Suggested Example Handlers

Examples are intended to demonstrate Relay, not to become built-in business features:
- `demo.echo` — simple deterministic handler.
- `demo.slow` — configurable duration to demonstrate timeout and worker concurrency.
- `demo.fail` — fails a configurable number of times to demonstrate retries.
- `demo.report` — illustrative report-like task.
- `demo.import` — illustrative batch task.

The demo handlers should be safe, deterministic where possible, and easy to run locally.

## 15. Roadmap (Post-MVP)

Potential future capabilities:
- Idempotency keys and deduplication.
- Job dependencies and workflow/DAG support.
- Recurring jobs / cron-like schedules.
- Rate limiting per queue or job type.
- Queue pause/resume.
- More advanced fairness policies.
- Additional storage or queue backends.
- SDKs for other languages.
- Authentication and authorization.
- Multi-instance scaling and deployment guidance.
- Enhanced job result storage.
- Retention policies and archival.
- CLI tooling.
- Plugin/extension model.

These are not MVP commitments.

## 16. Risks and Constraints

- At-least-once execution means handlers may run more than once.
- External side effects cannot be reliably rolled back by Relay.
- Database-based queue performance will depend on workload, indexes, and claim strategy.
- Long-running handlers and cancellation require careful context and resource management.
- Lease-based recovery may cause duplicate execution if a worker is slow or temporarily disconnected.
- Priority policies may lead to starvation without fairness mechanisms.
- Metrics and logs must avoid exposing sensitive payload data.
- The MVP is a local-first developer project, not a production-ready hosted service.

## 17. Glossary

- **Job:** Persisted unit of work.
- **Handler:** Application-defined implementation for processing a job type.
- **Queue:** Logical group of jobs.
- **Worker:** Process or instance that executes jobs.
- **Attempt:** A single execution of a job.
- **Lease:** Time-bounded claim/ownership signal for a running job.
- **Heartbeat:** Periodic worker signal indicating continued health/activity.
- **Backoff:** Increasing delay between retry attempts.
- **At-least-once:** Execution semantics where work may be executed more than once, but the system attempts not to lose accepted work.
