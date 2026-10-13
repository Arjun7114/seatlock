package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Arjun7114/seatlock/internal/db"
	"github.com/Arjun7114/seatlock/internal/repository"
)

// Domain errors — handlers map these to HTTP status codes.
var (
	ErrSeatNotFound  = errors.New("seat not found")
	ErrSeatBooked    = errors.New("seat already booked")
	ErrSeatHeld      = errors.New("seat is currently held")
	ErrNoHold        = errors.New("no active hold for this seat")
	ErrHoldOtherUser = errors.New("seat is held by another user")
)

// HoldTTL is how long a hold lasts before expiring.
const HoldTTL = 2 * time.Minute

type BookingService struct {
	repo *repository.Repository
}

func NewBookingService(repo *repository.Repository) *BookingService {
	return &BookingService{repo: repo}
}

// PlaceHold reserves a seat for a user for HoldTTL.
// NOTE: single-threaded correctness only. Concurrency hardening is Phase 4.
// PlaceHold reserves a seat for a user for HoldTTL.
// Uses row-level locking (SELECT ... FOR UPDATE) so concurrent holds on the
// same seat serialize cleanly instead of racing on the unique constraint.
func (s *BookingService) PlaceHold(ctx context.Context, seatID, userID int64) (int64, error) {
	var holdID int64

	err := s.repo.WithTx(ctx, func(q *db.Queries) error {
		// 1. Lock the seat row. Concurrent holds on this seat wait here.
		if _, err := q.LockSeat(ctx, seatID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSeatNotFound
			}
			return err
		}

		// 2. Already booked?
		if _, err := q.GetBookingBySeat(ctx, seatID); err == nil {
			return ErrSeatBooked
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 3. Already actively held by someone?
		if _, err := q.GetActiveHold(ctx, seatID); err == nil {
			return ErrSeatHeld
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 4. Clear any expired hold, then place a fresh one — all inside the lock.
		if err := q.DeleteHold(ctx, seatID); err != nil {
			return err
		}
		expiresAt := pgtype.Timestamptz{Time: time.Now().Add(HoldTTL), Valid: true}
		hold, err := q.CreateHold(ctx, db.CreateHoldParams{
			SeatID:    seatID,
			UserID:    userID,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return err
		}
		holdID = hold.ID
		return nil
	})

	if err != nil {
		return 0, err
	}
	return holdID, nil
}

// ConfirmBooking turns a valid hold into a booking.
func (s *BookingService) ConfirmBooking(ctx context.Context, seatID, userID int64) (int64, error) {
	hold, err := s.repo.GetActiveHold(ctx, seatID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrNoHold
		}
		return 0, err
	}
	if hold.UserID != userID {
		return 0, ErrHoldOtherUser
	}

	booking, err := s.repo.CreateBooking(ctx, db.CreateBookingParams{
		SeatID: seatID,
		UserID: userID,
	})
	if err != nil {
		return 0, err
	}
	_ = s.repo.DeleteHold(ctx, seatID) // best-effort cleanup
	return booking.ID, nil
}
// ConfirmBookingPessimistic confirms a booking using row-level locking
// (SELECT ... FOR UPDATE). Concurrent confirms on the same seat serialize:
// the first locks the seat row, the rest block until it commits, then see
// the booking already exists and lose cleanly.
func (s *BookingService) ConfirmBookingPessimistic(ctx context.Context, seatID, userID int64) (int64, error) {
	var bookingID int64

	err := s.repo.WithTx(ctx, func(q *db.Queries) error {
		// 1. Lock the seat row. Other txns on this seat wait here.
		if _, err := q.LockSeat(ctx, seatID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSeatNotFound
			}
			return err
		}

		// 2. Inside the lock: is it already booked?
		if _, err := q.GetBookingBySeat(ctx, seatID); err == nil {
			return ErrSeatBooked
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 3. Still free — create the booking.
		booking, err := q.CreateBooking(ctx, db.CreateBookingParams{
			SeatID: seatID,
			UserID: userID,
		})
		if err != nil {
			return err
		}
		bookingID = booking.ID

		// 4. Clean up any hold on this seat.
		return q.DeleteHold(ctx, seatID)
	})

	if err != nil {
		return 0, err
	}
	return bookingID, nil
}
// ConfirmBookingOptimistic confirms using optimistic concurrency control.
// It reads the seat's version, then atomically bumps it only if unchanged.
// If the version moved (someone else claimed it), this caller loses with
// ErrSeatBooked. No row locks are held — contenders don't block each other.
func (s *BookingService) ConfirmBookingOptimistic(ctx context.Context, seatID, userID int64) (int64, error) {
	var bookingID int64

	err := s.repo.WithTx(ctx, func(q *db.Queries) error {
		// 1. Read current seat + its version.
		seat, err := q.GetSeat(ctx, seatID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSeatNotFound
			}
			return err
		}

		// 2. Already booked? (fast path)
		if _, err := q.GetBookingBySeat(ctx, seatID); err == nil {
			return ErrSeatBooked
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// 3. Try to claim by bumping version — only succeeds if version unchanged.
		affected, err := q.ClaimSeatOptimistic(ctx, db.ClaimSeatOptimisticParams{
			ID:      seatID,
			Version: seat.Version,
		})
		if err != nil {
			return err
		}
		if affected == 0 {
			// Someone else bumped the version between our read and update.
			return ErrSeatBooked
		}

		// 4. We won the version race — create the booking.
		booking, err := q.CreateBooking(ctx, db.CreateBookingParams{
			SeatID: seatID,
			UserID: userID,
		})
		if err != nil {
			return err
		}
		bookingID = booking.ID
		return q.DeleteHold(ctx, seatID)
	})

	if err != nil {
		return 0, err
	}
	return bookingID, nil
}
// Confirm dispatches to the configured locking strategy.
func (s *BookingService) Confirm(ctx context.Context, strategy string, seatID, userID int64) (int64, error) {
	if strategy == "optimistic" {
		return s.ConfirmBookingOptimistic(ctx, seatID, userID)
	}
	return s.ConfirmBookingPessimistic(ctx, seatID, userID)
}