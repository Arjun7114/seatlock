# seatlock

A concurrent seat-reservation service in Go that **cannot double-book a seat under load** — with two interchangeable locking strategies, JWT authentication, and Redis-backed caching and rate limiting.

The interesting problem here isn't CRUD. It's correctness under contention: when hundreds of requests race for the last seat, exactly one must win and the rest must lose cleanly. seatlock solves that two different ways and measures the trade-off.

---

## Proof first

**Correctness** — 100 goroutines book the *same* seat simultaneously ([concurrency test](internal/service/booking_concurrency_test.go)):

| Strategy | Attempts | Succeeded | Conflicts | Double-bookings | DB rows |
|----------|----------|-----------|-----------|-----------------|---------|
| Pessimistic (`SELECT … FOR UPDATE`) | 100 | 1 | 99 | **0** | 1 |
| Optimistic (version CAS)            | 100 | 1 | 99 | **0** | 1 |

Verified against the actual `bookings` row count, not just the HTTP response.

**Throughput** — 200 concurrent clients, each doing hold → confirm:

| Scenario | Booked | Throughput | p50 | p95 | p99 |
|----------|--------|------------|-----|-----|-----|
| All 200 contend for 1 seat | 1  | 225 req/s | 735ms | 872ms | 885ms |
| Spread across 50 seats     | 50 | 593 req/s | 300ms | 331ms | 335ms |

The ~2.6× gap is the whole point: row locks on *different* seats don't block each other, so the system scales when demand is distributed and serializes correctly when it's concentrated. Measured locally (single machine, Dockerized PostgreSQL 17); reproduce via [METRICS.md](METRICS.md).

---

## How it works

A booking has two steps: a short-lived **hold** (TTL), then a **confirm** that turns the hold into a permanent booking.

POST /v1/seats/{id}/hold → reserve the seat for 2 minutes
POST /v1/seats/{id}/confirm → commit the booking


Each request runs inside a database transaction and takes a row-level lock on the seat:

```mermaid
flowchart LR
    C["Client"] -->|"POST /hold or /confirm"| API["chi handler"]
    API --> SVC["booking service"]
    SVC -->|"BEGIN tx"| TX["transaction"]
    TX -->|"SELECT ... FOR UPDATE"| LOCK[["lock seat row"]]
    LOCK --> CHK{"already booked<br/>or held?"}
    CHK -->|"yes"| ERR["409 Conflict"]
    CHK -->|"no"| WRITE["insert hold / booking"]
    WRITE -->|"COMMIT"| OK["201 Created"]
    WRITE -.->|"UNIQUE(seat_id)<br/>backstop"| DB[("PostgreSQL")]
```

When two clients race for the same seat, the row lock serializes them — one wins, one loses cleanly:

```mermaid
flowchart TB
    subgraph Race["Two clients, same seat"]
        A["Client A"] --> LOCKA["BEGIN, lock seat row FOR UPDATE"]
        B["Client B"] --> LOCKB["BEGIN, lock seat row FOR UPDATE"]
    end

    LOCKA --> AWIN["seat free, INSERT booking, COMMIT"]
    AWIN --> WIN["201 Created, A wins"]

    LOCKB --> WAIT["blocks until A commits"]
    WAIT --> BCHK["seat now booked"]
    BCHK --> LOSE["409 Conflict, B loses cleanly"]

    WIN --> RESULT["Exactly one booking, zero double-bookings"]
    LOSE --> RESULT
```

Two independent guarantees prevent double-booking:

1. **Application layer** — the chosen locking strategy (below).
2. **Database layer** — a `UNIQUE(seat_id)` constraint on `bookings` as a hard backstop. Even a logic bug can't produce two bookings for one seat.

## The two locking strategies

Selectable at runtime via the `LOCK_STRATEGY` env var (`pessimistic` default, or `optimistic`).

**Pessimistic — `SELECT … FOR UPDATE`.** The transaction locks the seat row up front; other transactions touching that seat block until it commits. By the time a waiter acquires the lock, the booking already exists and it loses cleanly. Best under **high contention** (everyone wants the same seat): no wasted work, but holders block.

**Optimistic — version compare-and-swap.** No locks held. Each caller reads the seat's `version`, then updates `… WHERE version = <read value>`. Exactly one update matches; the rest affect 0 rows and know they lost. Best under **low contention** (requests spread across many seats): no blocking, better throughput, but losers do wasted work and retry.

That "when would you pick which" is the real engineering content, and both are proven correct above.

## Authentication

- **Password hashing: Argon2id** (OWASP 2026 parameters — m=64 MiB, t=3, p=1), stored in self-describing PHC format with a per-password random salt and constant-time verification.
- **Tokens: JWT** (HS256, 24h expiry). Verification rejects non-HMAC algorithms to prevent algorithm-confusion attacks.
- Signup and login return a token; login gives a uniform error for unknown-email and wrong-password (no account enumeration).
- `hold` and `confirm` require a `Bearer` token — the user identity comes from the token, never the request body. Reads stay public.

## Caching & rate limiting

- **Cache-aside** on seat availability (`GET /v1/events/{id}/seats`): served from Redis with a 30s TTL and an `X-Cache: HIT|MISS` header, and **invalidated on every hold/booking** so availability is never stale.
- **Per-IP rate limiting** via Redis `INCR` (fixed window, 100 req/min), returning `429 Too Many Requests` with `Retry-After`.
- Both are **best-effort and fail open** — a Redis outage degrades to direct DB reads and unthrottled requests, never an error.

## Architecture

cmd/seatlock/ HTTP server entrypoint
cmd/loadtest/ concurrent load generator (hold→confirm, latency percentiles)
internal/
api/ chi router, handlers, auth + rate-limit middleware
service/ booking logic + the two locking strategies
repository/ pgx pool + WithTx transaction helper
db/ sqlc-generated, type-safe queries (DO NOT EDIT)
auth/ Argon2id hashing + JWT issue/verify
cache/ Redis client: cache-aside + rate limiter
config/ env-driven configuration
migrations/ golang-migrate SQL schema
scripts/ seed data


Layering is `handler → service → repository`. The `service` is where locking lives; handlers only parse input and map domain errors (`ErrSeatBooked`, `ErrSeatHeld`, `ErrNoHold`) to HTTP codes (409 / 422 / 404).

## Tech stack

Go · PostgreSQL 17 · Redis 7 · [pgx](https://github.com/jackc/pgx) · [sqlc](https://sqlc.dev) (type-safe queries) · [chi](https://github.com/go-chi/chi) · [golang-jwt](https://github.com/golang-jwt/jwt) · Argon2id · [golang-migrate](https://github.com/golang-migrate/migrate) · Docker Compose

## Running it

```bash
# 1. start Postgres + Redis
docker compose up -d

# 2. apply schema + seed (1 event, 50 seats)
migrate -path migrations -database "postgres://seatlock:seatlock@localhost:5432/seatlock?sslmode=disable" up
docker exec -i seatlock-db psql -U seatlock -d seatlock < scripts/seed.sql

# 3. run the server (default: pessimistic locking)
go run ./cmd/seatlock
# or: set LOCK_STRATEGY=optimistic

# 4. sign up, then book (identity comes from the token)
curl -X POST http://localhost:8080/v1/auth/signup -H "Content-Type: application/json" -d "{\"email\":\"a@b.com\",\"password\":\"supersecret\"}"
# take the returned token, then:
curl -X POST http://localhost:8080/v1/seats/1/hold    -H "Authorization: Bearer <token>"
curl -X POST http://localhost:8080/v1/seats/1/confirm -H "Authorization: Bearer <token>"
```

## API

| Method & path | Auth | Description |
|---------------|------|-------------|
| `GET  /healthz` | – | health check |
| `POST /v1/auth/signup` | – | register, returns a JWT |
| `POST /v1/auth/login` | – | log in, returns a JWT |
| `GET  /v1/events` | – | list events |
| `GET  /v1/events/{id}/seats` | – | seat availability (cached) |
| `POST /v1/seats/{id}/hold` | Bearer | place a hold (2-min TTL) |
| `POST /v1/seats/{id}/confirm` | Bearer | confirm a booking |

## Testing

```bash
go test ./...                                              # unit
go test ./internal/service/ -run TestConcurrentConfirm -v  # concurrency proof
go build ./cmd/loadtest && go run ./cmd/loadtest -workers 200 -contend  # load
```

## Scope & next steps

Not yet built, in priority order:

- **Observability** — Prometheus metrics and OpenTelemetry traces.
- **Containerized deploy** — multi-stage Dockerfile + Kubernetes manifests.
- **Hold expiry sweeper** — expired holds are currently cleared lazily on access; a background reaper would be cleaner.

> Note: the JWT secret falls back to a dev value locally; supply it via `JWT_SECRET` in any real deployment.