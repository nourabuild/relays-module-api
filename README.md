# Relays Service

Task-delegation API for the Noura platform. Users create tasks assigned to
*other* users (self-assignment is rejected), track them through an
`open → done / cancelled` lifecycle, and chat per-task over REST and
WebSocket.

Authentication is delegated to the external auth service: this service
verifies the HS256 JWTs the auth service signs and lazily mirrors users into
its own database on first request.

## Vocabulary

The two task lists are named from the caller's point of view:

- `GET /api/v1/task/todos` — tasks **you created** and delegated to others
- `GET /api/v1/task/expectations` — tasks **assigned to you**

## Status lifecycle

| From \ To   | open           | done          | cancelled     |
|-------------|----------------|---------------|---------------|
| `open`      | creator only   | assignee only | creator only  |
| `done`      | creator only   | 409 conflict  | 409 conflict  |
| `cancelled` | creator only   | 409 conflict  | 409 conflict  |

Terminal tasks (`done`/`cancelled`) cannot be edited; the creator must reopen
them first. Other users' profiles are serialized as public shapes only (no
email, phone, DOB, or city).

`PATCH /api/v1/task/:task_id` treats omitted fields as unchanged; an explicit
JSON `null` clears the nullable fields (`description`, `due_at`,
`delegated_from_task_id`).

## Layout

- `cmd/api/main.go` — entry point: service wiring, HTTP server, loopback
  debug/pprof server, graceful shutdown
- `internal/app` — Gin handlers and route registration
- `internal/sdk` — middleware, models, SQL repository, migrations
- `internal/services` — JWT verification, WebSocket rooms, Sentry

## Running locally

```bash
cp .env.example .env   # then fill in the values
make run               # starts the API on $PORT (default 10267)
```

The service **refuses to start** without a real `JWT_ACCESS_TOKEN_SECRET`
and `JWT_ISSUER` (placeholder values are rejected). Database connections
default to `sslmode=require`; set `BLUEPRINT_DB_SSLMODE=disable` only for a
local Postgres without TLS.

## Make targets

```bash
make run             # kill anything on $PORT and start the API
make test            # run the test suite (DB tests need Docker)
make lint            # go vet + staticcheck
make tidy            # go mod tidy + vendor
make migrate-up      # apply migrations (uses DATABASE_URL)
make migrate-down    # roll back migrations
make migrate-create  # create a new migration pair
```

CI (GitHub Actions) runs build, vet, staticcheck, tests, and govulncheck on
every push and pull request.
