-- Idempotent seed: one event with 50 seats (A1..A50).
-- Safe to run multiple times.

INSERT INTO events (id, name, venue, starts_at)
VALUES (1, 'Phoenix Live 2026', 'Arena Hall', now() + interval '30 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO seats (event_id, seat_label)
SELECT 1, 'A' || g
FROM generate_series(1, 50) AS g
ON CONFLICT (event_id, seat_label) DO NOTHING;

-- Keep the events id sequence ahead of our hardcoded id=1
SELECT setval('events_id_seq', (SELECT MAX(id) FROM events));