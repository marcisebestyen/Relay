# Relay
Mini background job orchestration platform

## Local infrastructure

Postgres runs in Docker so the backend can run natively against it.

```sh
cp relay-infrastructure/.env.example relay-infrastructure/.env   # then set POSTGRES_PASSWORD
make infra-up      # start Postgres, wait until healthy
make infra-down    # stop it; data is kept in a named volume
make infra-reset   # DESTRUCTIVE: stop and delete the volume (all local data)
```

Postgres listens on `localhost:5432`. Check health with
`docker inspect -f '{{.State.Health.Status}}' relay-postgres`.
Set `RELAY_DATABASE_URL` in `relay-backend/.env` to
`postgres://<user>:<password>@localhost:5432/<db>?sslmode=disable`.

## Database migrations

SQL migrations live in `relay-backend/migrations/` (Goose, embedded in the binary).
They need `RELAY_DATABASE_URL` set.

```sh
make migrate-up       # apply all pending migrations
make migrate-down     # roll back one migration
make migrate-status   # list migrations and their state
```

## API server

```sh
cd relay-backend && go run ./cmd/relay api   # listens on RELAY_HTTP_ADDR (default :8080)
```

| Route | Response |
|---|---|
| `GET /health` | `200 {"status":"ok"}` (process only, no DB) |
| `GET /ready` | `200 {"status":"ready"}`, or `503` error envelope with `details.failed` when the database ping fails |
| `GET /api/v1/openapi.yaml` | The OpenAPI 3 contract (`application/yaml`) |
| `POST /api/v1/jobs` | Submit a job, see below |
| `GET /api/v1/jobs/{id}` | `200` job, `400` invalid UUID, `404` unknown ID |
| `GET /api/v1/jobs/{id}/attempts` | Attempt history, see below |
| `GET /api/v1/jobs/{id}/events` | Job event timeline, see below |

Errors use `{"error": {"code", "message", "details"}}`. Every response has an `X-Request-ID` header.
SIGINT/SIGTERM drains in-flight requests for up to `RELAY_SHUTDOWN_TIMEOUT`.

## Submitting a job

```sh
curl -i -X POST localhost:8080/api/v1/jobs \
  -H 'Content-Type: application/json' \
  -d '{"handler":"demo.echo","queue":"default","payload":{"hello":"world"},"scheduled_at":"2030-01-02T03:04:05Z"}'
curl localhost:8080/api/v1/jobs/<id from the Location header>
```

Only `handler` is required. Optional: `payload` (any JSON, default `{}`), `queue` (default `default`),
`name`, `priority` (0-100, default 50), `scheduled_at` (RFC 3339; a past time runs right away),
`max_attempts` (default `RELAY_DEFAULT_MAX_ATTEMPTS`), `timeout_seconds` (default `RELAY_DEFAULT_TIMEOUT`).
Unknown fields are rejected.

| Status | When |
|---|---|
| `201` | Job stored as `pending`; `Location` points at it |
| `400 bad_request` | Body is not a single valid JSON object, or has an unknown field |
| `413 payload_too_large` | Body is larger than `RELAY_MAX_PAYLOAD_BYTES` |
| `422 validation_failed` | A field is invalid; `details` maps field name to message |

Relay does not check at submit time that a handler exists. Workers may register later, so an unknown
handler is accepted and fails when a worker picks it up.

## Attempts and events

```sh
curl 'localhost:8080/api/v1/jobs/<id>/attempts?limit=20'
curl 'localhost:8080/api/v1/jobs/<id>/events?cursor=<next_cursor from the previous page>'
```

Both return `{"items": [...], "next_cursor": ...}`. `next_cursor` is `null` on the last page; pass it back as
`cursor` for the next one. Cursors are opaque. `limit` is 1-200 (default 50).

- Attempts are ordered by `attempt_number`. `duration_ms` is derived from `started_at` and `finished_at`
  and is `null` while the attempt is `running`.
- Events are ordered by `created_at`, then `id`. `type` is one of the known values in the spec
  (`job.created`, `attempt.started`, `retry.scheduled`, ...); clients must tolerate new ones.
  `data` is small and never contains the job payload.

| Status | When |
|---|---|
| `200` | A page; an existing job with no attempts or events yet returns `"items": []` |
| `400 bad_request` | Invalid job UUID, non-numeric `limit`, or an invalid `cursor` |
| `404 not_found` | The job does not exist |
| `422 validation_failed` | `limit` is below 1 or above 200; `details.limit` has the message |

## OpenAPI contract

`relay-backend/api/openapi.yaml` is the source of truth (spec-first). Server types and the chi
interface in `relay-backend/internal/api/gen` are generated with `oapi-codegen`; never edit them by hand.
Every new endpoint extends the spec in the same change.

```sh
make generate         # regenerate after editing the spec
make generate-check   # fails if the generated code was out of date
```

## Tests

```sh
make test               # unit tests only
make test-integration   # all tests incl. integration; needs Docker (Testcontainers)
```
