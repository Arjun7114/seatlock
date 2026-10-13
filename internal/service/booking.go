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
func (s *BookingService) PlaceHold(ctx context.Context, seatID, userID int64) (int64, error) {
	if _, err := s.repo.GetSeat(ctx, seatID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrSeatNotFound
		}
		return 0, err
	}

	// Already booked?
	if _, err := s.repo.GetBookingBySeat(ctx, seatID); err == nil {
		return 0, ErrSeatBooked
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	// Already actively held?
	if _, err := s.repo.GetActiveHold(ctx, seatID); err == nil {
		return 0, ErrSeatHeld
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	// Clear any expired hold, then place a fresh one.
	if err := s.repo.DeleteHold(ctx, seatID); err != nil {
		return 0, err
	}

	expiresAt := pgtype.Timestamptz{Time: time.Now().Add(HoldTTL), Valid: true}
	hold, err := s.repo.CreateHold(ctx, db.CreateHoldParams{
		SeatID:    seatID,
		UserID:    userID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return 0, err
	}
	return hold.ID, nil
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