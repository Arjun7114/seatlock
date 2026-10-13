-- name: ListEvents :many
SELECT id, name, venue, starts_at, created_at
FROM events
ORDER BY starts_at;

-- name: GetEvent :one
SELECT id, name, venue, starts_at, created_at
FROM events
WHERE id = $1;

-- name: ListSeatsForEvent :many
SELECT
    s.id,
    s.seat_label,
    s.version,
    (b.id IS NOT NULL) AS is_booked
FROM seats s
LEFT JOIN bookings b ON b.seat_id = s.id
WHERE s.event_id = $1
ORDER BY s.id;