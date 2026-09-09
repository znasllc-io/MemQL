package webauthn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

var (
	// ErrChallengeNotFound covers both "never existed" and "already
	// redeemed". They are deliberately indistinguishable to the caller:
	// telling a client which one it hit turns the handle into an oracle
	// for whether a ceremony is in flight.
	ErrChallengeNotFound = errors.New("webauthn: challenge not found or already used")

	// ErrChallengeExpired is separated from NotFound because the client
	// action differs -- an expired challenge means "start again", which
	// is worth saying so a slow user is not left guessing.
	ErrChallengeExpired = errors.New("webauthn: challenge expired")

	// ErrChallengeUserMismatch fires when the finishing caller is not the
	// user the challenge was minted for.
	ErrChallengeUserMismatch = errors.New("webauthn: challenge was issued to a different user")
)

// ChallengeEntry is one in-flight ceremony.
type ChallengeEntry struct {
	// UserId is the v1:identity:user the challenge was minted for. For
	// registration this is the authenticated caller; the login ceremony
	// (memql#3407) mints discoverable challenges with an empty UserId,
	// because there is no known user yet.
	UserId string

	// Ceremony is CeremonyRegister or CeremonyLogin.
	Ceremony string

	// Session is go-webauthn's server-side ceremony state: the
	// challenge bytes, the RP ID, the user handle and the UV
	// requirement. This is the half the client must never see.
	Session *gowebauthn.SessionData

	// OAuth is the relying-party context a LOGIN challenge carries
	// (memql#3407); zero for a registration challenge. It lives here
	// rather than travelling back through the client for the same
	// reason Session does: these five fields decide where the minted
	// auth code is delivered and what verifier can redeem it, so a
	// client that could restate them at finish time could redirect
	// someone else's code to itself.
	OAuth OAuthContext

	// ExpiresAt is when Take starts refusing it.
	ExpiresAt time.Time
}

// ChallengeStore holds in-flight ceremonies, keyed by an opaque handle.
//
// HTTP ceremonies use shared Postgres persistence: begin and finish can
// reach different replicas. The storage key binds the handle to the trusted
// RP ID and configured origin, so another front door cannot consume it.
//
// The properties that matter, and where each is enforced:
//
//   - Server-minted challenge: go-webauthn's protocol.CreateChallenge,
//     never a client-supplied value.
//   - Single-use: Take deletes the entry BEFORE it validates anything,
//     so a failed finish burns the challenge exactly as a successful one
//     does. Replay of a captured response has nothing to replay against.
//   - TTL'd: every entry carries an absolute expiry; Take refuses past
//     it and Put opportunistically sweeps.
//   - Bound: the entry carries the user the challenge was minted for and
//     the ceremony it belongs to, and Take checks the ceremony tag.
type ChallengeStore struct {
	backend ChallengeBackend
	scope   string
	ttl     time.Duration
	now     func() time.Time
}

// NewChallengeStore builds a store with the given TTL. A nil clock
// defaults to time.Now.
func NewChallengeStore(ttl time.Duration, now func() time.Time) *ChallengeStore {
	if ttl <= 0 {
		ttl = DefaultChallengeTTL
	}
	if now == nil {
		now = time.Now
	}
	return &ChallengeStore{
		backend: NewMemoryChallengeBackend(),
		ttl:     ttl,
		now:     now,
	}
}

// TTL reports the configured challenge lifetime.
func (s *ChallengeStore) TTL() time.Duration { return s.ttl }

// Put stores a ceremony session and returns its opaque handle plus the
// expiry. The handle is 32 bytes of crypto/rand: it is a capability
// naming one in-flight ceremony, so it must not be guessable even though
// it is useless without a matching authenticator response.
//
// oauth is the login ceremony's relying-party context; registration
// passes the zero value.
func (s *ChallengeStore) Put(userId, ceremony string, session *gowebauthn.SessionData, oauth OAuthContext) (string, time.Time, error) {
	if session == nil {
		return "", time.Time{}, errors.New("webauthn: challenge store: session required")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", time.Time{}, err
	}
	handle := base64.RawURLEncoding.EncodeToString(buf)
	now := s.now().UTC()
	expiresAt := now.Add(s.ttl)

	entry := &ChallengeEntry{
		UserId:    strings.TrimSpace(userId),
		Ceremony:  ceremony,
		Session:   session,
		OAuth:     oauth,
		ExpiresAt: expiresAt,
	}
	if err := s.backend.Put(s.key(handle), entry, now); err != nil {
		return "", time.Time{}, err
	}
	return handle, expiresAt, nil
}

// Take consumes the challenge named by handle.
//
// The entry is deleted on EVERY path, including the ones that then
// return an error. That is the single-use property, and it is the reason
// this is Take rather than Get: a caller that could look without
// consuming would eventually be written as look-then-consume, and the
// window between the two is exactly the replay window.
func (s *ChallengeStore) Take(handle, ceremony string) (*ChallengeEntry, error) {
	handle = strings.TrimSpace(handle)
	if handle == "" {
		return nil, ErrChallengeNotFound
	}
	entry, err := s.backend.Take(s.key(handle))
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, ErrChallengeNotFound
	}
	if entry.Ceremony != ceremony {
		// A registration challenge presented as a login assertion (or the
		// reverse) is not a stale handle, it is a confused-deputy attempt.
		// It reads as "not found" for the same reason the two cases above
		// do -- no oracle -- and it is already consumed either way.
		return nil, ErrChallengeNotFound
	}
	if !s.now().UTC().Before(entry.ExpiresAt) {
		return nil, ErrChallengeExpired
	}
	return entry, nil
}

func (s *ChallengeStore) key(handle string) string {
	digest := sha256.Sum256([]byte(s.scope + "\x00" + handle))
	return hex.EncodeToString(digest[:])
}
