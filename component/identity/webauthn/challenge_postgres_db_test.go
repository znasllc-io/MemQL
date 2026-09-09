package webauthn

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/znasllc-io/memql/component/database/dbtest"
)

func challengeDB(t *testing.T) *sql.DB {
	t.Helper()
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN())))
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "shared passkey challenges", dbtest.DSN(), err)
		return nil
	}
	return db
}

func sqlCeremony(t *testing.T, db *sql.DB) *Ceremony {
	t.Helper()
	c, err := New(Config{BaseURL: testBaseURL, ChallengeBackend: NewPostgresChallengeBackend(func() *sql.DB { return db })})
	require.NoError(t, err)
	return c
}

func TestPostgresChallengePreservesServerSideState(t *testing.T) {
	db := challengeDB(t)
	first, second := sqlCeremony(t, db), sqlCeremony(t, db)
	session := &gowebauthn.SessionData{
		Challenge: "server-random-challenge", RelyingPartyID: testRPID, Origin: testOrigin,
		UserID: []byte(testUserId), AllowedCredentialIDs: [][]byte{{0, 255, 42}},
		Expires: time.Now().UTC().Add(time.Minute), UserVerification: protocol.VerificationRequired,
		CredParams: []protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}},
	}
	oauth := OAuthContext{ClientId: "os", RedirectURI: "https://os.example.test/callback", State: "state", CodeChallenge: "pkce", CodeChallengeMethod: "S256"}
	handle, expires, err := first.challenges.Put(testUserId, CeremonyLogin, session, oauth)
	require.NoError(t, err)
	var storedKey string
	err = db.QueryRow(`SELECT handle_hash FROM identity_webauthn_challenges WHERE handle_hash = $1`, first.challenges.key(handle)).Scan(&storedKey)
	require.NoError(t, err)
	require.NotEqual(t, handle, storedKey)
	entry, err := second.challenges.Take(handle, CeremonyLogin)
	require.NoError(t, err)
	require.Equal(t, session, entry.Session)
	require.Equal(t, oauth, entry.OAuth)
	require.Equal(t, testUserId, entry.UserId)
	require.Equal(t, expires, entry.ExpiresAt)
	_, err = first.challenges.Take(handle, CeremonyLogin)
	require.ErrorIs(t, err, ErrChallengeNotFound)
}

func TestPostgresChallengeConcurrentSignedLoginHasOneWinner(t *testing.T) {
	db := challengeDB(t)
	first := sqlCeremony(t, db)
	a := newSoftwareAuthenticator(t)
	row := storedRow(a, testUserId, 0)
	challenge, err := first.BeginLogin(OAuthContext{})
	require.NoError(t, err)
	a.signCount = 1
	body := a.Assert(challenge.Options.Response.Challenge.String(), testRPID, testOrigin, testUserId)
	const contenders = 16
	start := make(chan struct{})
	results := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		// Independent stores and ceremonies, not a mutex shared by all callers.
		replica := sqlCeremony(t, db)
		go func() {
			<-start
			_, err := replica.FinishLogin(challenge.ChallengeId, bytes.NewReader(body), resolverFor(row))
			results <- err
		}()
	}
	close(start)
	winners := 0
	for i := 0; i < contenders; i++ {
		err := <-results
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrChallengeNotFound)
		}
	}
	require.Equal(t, 1, winners)
}

func TestPostgresChallengeFailedFinishStillConsumes(t *testing.T) {
	db := challengeDB(t)
	for _, name := range []string{"wrong ceremony", "expired", "wrong user", "malformed assertion"} {
		t.Run(name, func(t *testing.T) {
			first, second := sqlCeremony(t, db), sqlCeremony(t, db)
			registration, err := first.BeginRegistration(&User{Id: testUserId})
			require.NoError(t, err)
			handle := registration.ChallengeId
			switch name {
			case "wrong ceremony":
				_, err = second.challenges.Take(handle, CeremonyLogin)
				require.ErrorIs(t, err, ErrChallengeNotFound)
			case "expired":
				second.challenges.now = func() time.Time { return registration.ExpiresAt }
				_, err = second.challenges.Take(handle, CeremonyRegister)
				require.ErrorIs(t, err, ErrChallengeExpired)
			case "wrong user":
				_, err = second.FinishRegistration(handle, "v1:identity:user:other", strings.NewReader(`{}`))
				require.ErrorIs(t, err, ErrChallengeUserMismatch)
			case "malformed assertion":
				_, err = second.FinishRegistration(handle, testUserId, strings.NewReader(`{}`))
				require.ErrorContains(t, err, "attestation response rejected")
			}
			_, err = first.challenges.Take(handle, CeremonyRegister)
			require.ErrorIs(t, err, ErrChallengeNotFound)
		})
	}
}

func TestPostgresChallengeCannotBeConsumedByAnotherRelyingParty(t *testing.T) {
	db := challengeDB(t)
	first := sqlCeremony(t, db)
	for _, scope := range []struct{ rp, origin string }{
		{testRPID, "https://identity.test:8443"},
		{"other.test", testOrigin},
	} {
		other, err := newCeremony(scope.rp, scope.origin, Config{ChallengeBackend: NewPostgresChallengeBackend(func() *sql.DB { return db })})
		require.NoError(t, err)
		challenge, err := first.BeginLogin(OAuthContext{})
		require.NoError(t, err)
		_, err = other.challenges.Take(challenge.ChallengeId, CeremonyLogin)
		require.ErrorIs(t, err, ErrChallengeNotFound)
		_, err = first.challenges.Take(challenge.ChallengeId, CeremonyLogin)
		require.NoError(t, err, "a foreign scope must not burn the source challenge")
	}
}

func TestPostgresChallengeDatabaseFailureHasNoLocalFallback(t *testing.T) {
	db := challengeDB(t)
	var current *sql.DB
	c, err := New(Config{BaseURL: testBaseURL, ChallengeBackend: NewPostgresChallengeBackend(func() *sql.DB { return current })})
	require.NoError(t, err)
	challenge, err := c.BeginLogin(OAuthContext{})
	require.ErrorContains(t, err, "storage unavailable")
	require.Nil(t, challenge)
	current = db
	challenge, err = c.BeginLogin(OAuthContext{})
	require.NoError(t, err, "the same cached ceremony recovers once storage returns")
	current = nil
	_, err = c.challenges.Take(challenge.ChallengeId, CeremonyLogin)
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrChallengeNotFound), "an outage is not a missing challenge")
	current = db
	_, err = c.challenges.Take(challenge.ChallengeId, CeremonyLogin)
	require.NoError(t, err)
}

func TestPostgresChallengeReportsQueryFailuresAsStorageErrors(t *testing.T) {
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN())))
	require.NoError(t, db.Close())
	c := sqlCeremony(t, db)
	challenge, err := c.BeginLogin(OAuthContext{})
	require.ErrorIs(t, err, ErrChallengeStorage)
	require.Nil(t, challenge)
	_, err = c.challenges.Take("unknown-handle", CeremonyLogin)
	require.ErrorIs(t, err, ErrChallengeStorage)
	require.False(t, errors.Is(err, ErrChallengeNotFound))
}

func TestPostgresChallengeCleanupIsBoundedAndPreservesLiveEntries(t *testing.T) {
	db := challengeDB(t)
	c := sqlCeremony(t, db)
	prefix := c.challenges.key(rand.Text()) + ":"
	_, err := db.Exec(`INSERT INTO identity_webauthn_challenges (handle_hash, payload, expires_at)
		SELECT $1 || n::text, '{}'::jsonb, $2 FROM generate_series(1, 256) AS n`, prefix, time.Now().UTC().Add(-time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM identity_webauthn_challenges WHERE handle_hash LIKE $1`, prefix+"%")
	})
	challenge, err := c.BeginLogin(OAuthContext{})
	require.NoError(t, err)
	var remaining int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM identity_webauthn_challenges WHERE handle_hash LIKE $1`, prefix+"%").Scan(&remaining))
	require.GreaterOrEqual(t, remaining, 128, "one begin must not delete an unbounded backlog")
	require.Less(t, remaining, 256, "abandoned expired ceremonies must be collected")
	_, err = sqlCeremony(t, db).challenges.Take(challenge.ChallengeId, CeremonyLogin)
	require.NoError(t, err, "cleanup must preserve unexpired challenges")
}
