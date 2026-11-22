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

Errors use `{"error": {"code", "message", "details"}}`. Every response has an `X-Request-ID` header.
SIGINT/SIGTERM drains in-flight requests for up to `RELAY_SHUTDOWN_TIMEOUT`.

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
