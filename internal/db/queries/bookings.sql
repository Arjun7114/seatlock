-- name: GetSeat :one
SELECT id, event_id, seat_label, version
FROM seats
WHERE id = $1;

-- name: GetActiveHold :one
SELECT id, seat_id, user_id, expires_at, created_at
FROM holds
WHERE seat_id = $1 AND expires_at > now();

-- name: CreateHold :one
INSERT INTO holds (seat_id, user_id, expires_at)
VALUES ($1, $2, $3)
RETURNING id, seat_id, user_id, expires_at, created_at;

-- name: DeleteHold :exec
DELETE FROM holds WHERE seat_id = $1;

-- name: GetBookingBySeat :one
SELECT id, seat_id, user_id, created_at
FROM bookings
WHERE seat_id = $1;

-- name: CreateBooking :one
INSERT INTO bookings (seat_id, user_id)
VALUES ($1, $2)
RETURNING id, seat_id, user_id, created_at;
-- name: LockSeat :one
-- Acquires a row-level lock on the seat; concurrent txns on the same seat block here.
SELECT id, event_id, seat_label, version
FROM seats
WHERE id = $1
FOR UPDATE;
-- name: ClaimSeatOptimistic :execrows
-- Bumps the seat's version only if it still matches what the caller read.
-- Returns rows-affected: 1 = this caller won, 0 = someone else already moved it.
UPDATE seats
SET version = version + 1
WHERE id = $1 AND version = $2;