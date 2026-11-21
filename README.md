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
