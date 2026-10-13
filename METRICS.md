# seatlock — Measured Results

All figures measured locally: single machine, Dockerized PostgreSQL 17,
Go load generator (`cmd/loadtest`), 200 concurrent clients per run.
Reproduce with the commands under "How to reproduce" below.

## Correctness under concurrency

100 goroutines attempt to book the **same seat** simultaneously
(`internal/service/booking_concurrency_test.go`):

| Strategy | Attempts | Succeeded | Conflicts | Double-bookings | DB rows |
|----------|----------|-----------|-----------|-----------------|---------|
| Pessimistic (`SELECT ... FOR UPDATE`) | 100 | 1 | 99 | 0 | 1 |
| Optimistic (version CAS)              | 100 | 1 | 99 | 0 | 1 |

Zero double-bookings under 100-way contention, verified against the actual
`bookings` row count — not just the API response.

## Throughput & latency (HTTP, pessimistic strategy)

200 concurrent clients, each running hold → confirm:

| Scenario | Booked | Conflicts | Throughput | p50 | p95 | p99 |
|----------|--------|-----------|------------|-----|-----|-----|
| Max contention (all 200 → 1 seat) | 1  | 199 | 225 req/s | 735ms | 872ms | 885ms |
| Spread (200 → 50 seats)           | 50 | 150 | 593 req/s | 300ms | 331ms | 335ms |

The ~2.6× throughput gap between the two scenarios reflects lock contention:
locks on *different* seats don't block each other, so the system scales when
demand is distributed and serializes correctly when it's concentrated.

## How to reproduce

```
# start DB + server
docker compose up -d
go run ./cmd/seatlock

# correctness
go test ./internal/service/ -run TestConcurrentConfirm -v

# load (reset DB between runs)
go run ./cmd/loadtest -workers 200 -contend      # max contention
go run ./cmd/loadtest -workers 200 -seats 50     # spread
```