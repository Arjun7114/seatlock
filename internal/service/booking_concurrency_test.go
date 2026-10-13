package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Arjun7114/seatlock/internal/config"
	"github.com/Arjun7114/seatlock/internal/db"
	"github.com/Arjun7114/seatlock/internal/repository"
)

// setupTest connects to the dev DB and returns a clean, booked-free seat to race on.
func setupTest(t *testing.T) (*BookingService, *pgxpool.Pool, int64) {
	t.Helper()
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, config.DatabaseURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Pick a fresh seat for this test and wipe any state on it.
	q := db.New(pool)
	var seatID int64
	err = pool.QueryRow(ctx,
		`SELECT id FROM seats WHERE event_id = 1 ORDER BY id DESC LIMIT 1`,
	).Scan(&seatID)
	if err != nil {
		t.Fatalf("pick seat: %v", err)
	}
	// Clean slate: remove any booking/hold on this seat.
	if _, err := pool.Exec(ctx, `DELETE FROM bookings WHERE seat_id = $1`, seatID); err != nil {
		t.Fatalf("clean bookings: %v", err)
	}
	if err := q.DeleteHold(ctx, seatID); err != nil {
		t.Fatalf("clean holds: %v", err)
	}

	repo := repository.New(pool)
	return NewBookingService(repo), pool, seatID
}

func TestConcurrentConfirm_Pessimistic(t *testing.T) {
	svc, pool, seatID := setupTest(t)
	defer pool.Close()
	ctx := context.Background()

	const goroutines = 100

	var (
		wg        sync.WaitGroup
		start     = make(chan struct{}) // released to fire all at once
		mu        sync.Mutex
		succeeded int
		conflicts int
		other     []error
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			<-start // wait for the gun
			_, err := svc.ConfirmBookingPessimistic(ctx, seatID, userID)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrSeatBooked):
				conflicts++
			default:
				other = append(other, err)
			}
		}(1) // user ids 1..5 (all seeded? only user 1 is — see note)
	}

	close(start) // GO
	wg.Wait()

	// Count actual booking rows in the DB — the ground truth.
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE seat_id = $1`, seatID,
	).Scan(&rows); err != nil {
		t.Fatalf("count bookings: %v", err)
	}

	t.Logf("RESULTS: seat=%d goroutines=%d succeeded=%d conflicts=%d other_errors=%d booking_rows=%d",
		seatID, goroutines, succeeded, conflicts, len(other), rows)

	if len(other) > 0 {
		t.Errorf("unexpected errors (want only ErrSeatBooked): %v", other[0])
	}
	if succeeded != 1 {
		t.Errorf("want exactly 1 success, got %d", succeeded)
	}
	if rows != 1 {
		t.Errorf("want exactly 1 booking row, got %d — DOUBLE BOOKING!", rows)
	}
}
func TestConcurrentConfirm_Optimistic(t *testing.T) {
	svc, pool, seatID := setupTest(t)
	defer pool.Close()
	ctx := context.Background()

	// Reset the seat's version so the race starts clean.
	if _, err := pool.Exec(ctx, `UPDATE seats SET version = 0 WHERE id = $1`, seatID); err != nil {
		t.Fatalf("reset version: %v", err)
	}

	const goroutines = 100

	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		mu        sync.Mutex
		succeeded int
		conflicts int
		other     []error
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.ConfirmBookingOptimistic(ctx, seatID, 1)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrSeatBooked):
				conflicts++
			default:
				other = append(other, err)
			}
		}()
	}

	close(start)
	wg.Wait()

	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM bookings WHERE seat_id = $1`, seatID,
	).Scan(&rows); err != nil {
		t.Fatalf("count bookings: %v", err)
	}

	t.Logf("RESULTS (optimistic): seat=%d goroutines=%d succeeded=%d conflicts=%d other_errors=%d booking_rows=%d",
		seatID, goroutines, succeeded, conflicts, len(other), rows)

	if len(other) > 0 {
		t.Errorf("unexpected errors: %v", other[0])
	}
	if succeeded != 1 {
		t.Errorf("want exactly 1 success, got %d", succeeded)
	}
	if rows != 1 {
		t.Errorf("want exactly 1 booking row, got %d — DOUBLE BOOKING!", rows)
	}
}