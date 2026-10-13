-- Users who make bookings
CREATE TABLE users (
    id          BIGSERIAL PRIMARY KEY,
    email       TEXT        NOT NULL UNIQUE,
    password_hash TEXT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A bookable event (a concert, a show)
CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    venue       TEXT        NOT NULL,
    starts_at   TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Individual seats belonging to an event
CREATE TABLE seats (
    id          BIGSERIAL PRIMARY KEY,
    event_id    BIGINT      NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    seat_label  TEXT        NOT NULL,         -- e.g. "A12"
    version     INTEGER     NOT NULL DEFAULT 0, -- for optimistic locking later
    CONSTRAINT uq_seat_per_event UNIQUE (event_id, seat_label)
);

-- A short-lived hold placed on a seat before confirmation
CREATE TABLE holds (
    id          BIGSERIAL PRIMARY KEY,
    seat_id     BIGINT      NOT NULL REFERENCES seats(id) ON DELETE CASCADE,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_active_hold_per_seat UNIQUE (seat_id)
);

-- A confirmed booking. UNIQUE(seat_id) is the hard DB-level guarantee
-- that a seat can be booked at most once.
CREATE TABLE bookings (
    id          BIGSERIAL PRIMARY KEY,
    seat_id     BIGINT      NOT NULL REFERENCES seats(id) ON DELETE CASCADE,
    user_id     BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_booking_per_seat UNIQUE (seat_id)
);

CREATE INDEX idx_seats_event_id   ON seats(event_id);
CREATE INDEX idx_holds_expires_at ON holds(expires_at);