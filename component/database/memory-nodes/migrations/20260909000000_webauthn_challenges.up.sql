-- Ephemeral server-side ceremony state. This is an internal authentication
-- substrate, not a graph concept: it must never be queried or broadcast to
-- clients, and DELETE RETURNING provides atomic single-use consumption.
CREATE TABLE IF NOT EXISTS identity_webauthn_challenges (
    handle_hash text PRIMARY KEY,
    payload jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS identity_webauthn_challenges_expiry_idx
    ON identity_webauthn_challenges (expires_at, handle_hash);
