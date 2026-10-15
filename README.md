# seatlock

A concurrent seat-reservation service in Go that **cannot double-book a seat under load** — demonstrated with 200 concurrent clients and two interchangeable locking strategies, benchmarked against each other.

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

## Architecture

cmd/seatlock/ HTTP server entrypoint
cmd/loadtest/ concurrent load generator (hold→confirm, latency percentiles)
internal/
api/ chi router, handlers, middleware, domain-error → HTTP mapping
service/ booking logic + the two locking strategies
repository/ pgx pool + WithTx transaction helper
db/ sqlc-generated, type-safe queries (DO NOT EDIT)
config/ env-driven configuration
migrations/ golang-migrate SQL schema
scripts/ seed data


Layering is `handler → service → repository`. The `service` is where locking lives; handlers only parse input and map domain errors (`ErrSeatBooked`, `ErrSeatHeld`, `ErrNoHold`) to HTTP codes (409 / 422 / 404).

## Tech stack

Go · PostgreSQL 17 · [pgx](https://github.com/jackc/pgx) · [sqlc](https://sqlc.dev) (type-safe queries) · [chi](https://github.com/go-chi/chi) · [golang-migrate](https://github.com/golang-migrate/migrate) · Docker Compose

## Running it

```bash
# 1. start Postgres
docker compose up -d

# 2. apply schema + seed (1 event, 50 seats)
migrate -path migrations -database "postgres://seatlock:seatlock@localhost:5432/seatlock?sslmode=disable" up
docker exec -i seatlock-db psql -U seatlock -d seatlock < scripts/seed.sql

# 3. run the server (default: pessimistic locking)
go run ./cmd/seatlock
# or: set LOCK_STRATEGY=optimistic

# 4. try it
curl http://localhost:8080/v1/events/1/seats
curl -X POST http://localhost:8080/v1/seats/1/hold    -H "Content-Type: application/json" -d "{\"user_id\":1}"
curl -X POST http://localhost:8080/v1/seats/1/confirm -H "Content-Type: application/json" -d "{\"user_id\":1}"
```

## API

| Method & path | Description |
|---------------|-------------|
| `GET  /healthz` | health check |
| `GET  /v1/events` | list events |
| `GET  /v1/events/{id}/seats` | seat availability |
| `POST /v1/seats/{id}/hold` | place a hold (2-min TTL) |
| `POST /v1/seats/{id}/confirm` | confirm a booking |

## Testing

```bash
go test ./...                                              # unit
go test ./internal/service/ -run TestConcurrentConfirm -v  # concurrency proof
go build ./cmd/loadtest && go run ./cmd/loadtest -workers 200 -contend  # load
```

## Scope & next steps

Deliberately scoped to make the concurrency problem the centerpiece. Not yet built, in priority order:

- **Authentication** — currently `user_id` is passed in the request body; to be replaced with JWT-based identity.
- **Caching** — Redis for seat-availability reads; per-user rate limiting.
- **Observability** — Prometheus metrics and OpenTelemetry traces.
- **Hold expiry sweeper** — expired holds are currently cleared lazily on access; a background reaper would be cleaner.