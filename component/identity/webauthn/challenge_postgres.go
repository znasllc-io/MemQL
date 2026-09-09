package webauthn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type postgresChallengeBackend struct{ db func() *sql.DB }

// ErrChallengeStorage identifies an infrastructure failure, not a bad passkey.
// HTTP handlers report it as retryable and keep database details in logs.
var ErrChallengeStorage = errors.New("webauthn: shared challenge storage unavailable")

// NewPostgresChallengeBackend shares in-flight ceremonies across identity
// replicas. Resolve the connection per operation: a transient boot-time
// outage must not become a permanently cached failure. Each operation has a
// finite deadline, including connection acquisition and row-lock waits.
func NewPostgresChallengeBackend(db func() *sql.DB) ChallengeBackend {
	return &postgresChallengeBackend{db: db}
}

func (s *postgresChallengeBackend) connection() (*sql.DB, error) {
	if s.db != nil {
		if db := s.db(); db != nil {
			return db, nil
		}
	}
	return nil, ErrChallengeStorage
}

func (s *postgresChallengeBackend) Put(key string, entry *ChallengeEntry, now time.Time) error {
	db, err := s.connection()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("%w: encode: %w", ErrChallengeStorage, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Bound opportunistic cleanup. SKIP LOCKED lets concurrent begins avoid
	// waiting on the same expired entries. Nothing is broadcast to clients.
	_, err = db.ExecContext(ctx, `WITH expired AS (
		SELECT handle_hash FROM identity_webauthn_challenges
		WHERE expires_at <= $1 ORDER BY expires_at, handle_hash
		LIMIT 128 FOR UPDATE SKIP LOCKED
	), removed AS (
		DELETE FROM identity_webauthn_challenges WHERE handle_hash IN (SELECT handle_hash FROM expired)
	)
	INSERT INTO identity_webauthn_challenges (handle_hash, payload, expires_at)
	VALUES ($2, $3::jsonb, $4)`, now, key, string(payload), entry.ExpiresAt)
	if err != nil {
		return fmt.Errorf("%w: store: %w", ErrChallengeStorage, err)
	}
	return nil
}

func (s *postgresChallengeBackend) Take(key string) (*ChallengeEntry, error) {
	db, err := s.connection()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var payload []byte
	// One statement is the single-use guarantee across all replicas. Do not
	// predicate on ceremony or expiry: even a failed finish burns its handle.
	err = db.QueryRowContext(ctx, `DELETE FROM identity_webauthn_challenges
		WHERE handle_hash = $1 RETURNING payload`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChallengeNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("%w: consume: %w", ErrChallengeStorage, err)
	}
	var entry ChallengeEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return nil, fmt.Errorf("%w: decode: %w", ErrChallengeStorage, err)
	}
	if entry.Session == nil {
		return nil, fmt.Errorf("%w: stored challenge has no session", ErrChallengeStorage)
	}
	return &entry, nil
}
