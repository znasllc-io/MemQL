package http

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/bun/driver/pgdriver"
	"github.com/znasllc-io/memql/component/database/dbtest"
	"github.com/znasllc-io/memql/component/identity"
	"github.com/znasllc-io/memql/component/identity/webauthn"
	memqlengine "github.com/znasllc-io/memql/component/memql"
)

// Begin and finish deliberately use independent Servers, just as two
// identity pods do. The assertion is really signed; only credential and
// OAuth persistence use the existing handler fixture.
func passkeyReplicaDB(t *testing.T) *sql.DB {
	t.Helper()
	db := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dbtest.DSN())))
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		dbtest.Unreachable(t, "passkey replica challenge", dbtest.DSN(), err)
		return nil
	}
	return db
}

func TestPasskeyLoginFinishesOnAnotherIdentityReplica(t *testing.T) {
	db := passkeyReplicaDB(t)
	a := newHTTPSoftwareAuthenticator(t)
	first, _ := newPasskeyLoginServer(t)
	second, engine := newPasskeyLoginServer(t, loginPasskeyRow(a, passkeyTestUserId, 0, true))
	first.ChallengeBackend = nil
	second.ChallengeBackend = nil
	first.Store.DirectDB = func() *sql.DB { return db }
	second.Store.DirectDB = func() *sql.DB { return db }
	begin := beginPasskeyLogin(t, first, pkceBeginRequest())
	require.Equal(t, http.StatusOK, begin.Code, begin.Body.String())
	challenge := decodeLoginBegin(t, begin)
	finish := drivePasskey(t, second, "/auth/webauthn/login/finish", "", WebAuthnLoginFinishRequest{
		ChallengeId: challenge.ChallengeId,
		Credential:  a.assert(challenge.RequestOptions.Response.Challenge.String(), passkeyTestUserId),
	}, second.handleWebAuthnLoginFinish)
	require.Equal(t, http.StatusOK, finish.Code, "finish on the other replica: %s", finish.Body.String())
	finished := decodeLoginFinish(t, finish)
	require.True(t, finished.Success)
	require.Equal(t, passkeyLoginChallenge, engine.authCode["codeChallenge"])
	require.Equal(t, "S256", engine.authCode["codeChallengeMethod"])
	require.Equal(t, passkeyLoginState, engine.authCode["state"])
	target, err := url.Parse(finished.RedirectTo)
	require.NoError(t, err)
	token := postToken(t, second, url.Values{
		"grant_type": {"authorization_code"}, "code": {target.Query().Get("code")},
		"client_id": {passkeyLoginClientId}, "redirect_uri": {passkeyLoginRedirectURI},
		"code_verifier": {passkeyLoginVerifier},
	})
	require.Equal(t, http.StatusOK, token.Code, token.Body.String())
	replay := drivePasskey(t, first, "/auth/webauthn/login/finish", "", WebAuthnLoginFinishRequest{
		ChallengeId: challenge.ChallengeId, Credential: []byte(`{}`),
	}, first.handleWebAuthnLoginFinish)
	require.Equal(t, "challenge_not_found", decodeLoginFinish(t, replay).ErrorCode)
}

func TestPasskeyRegistrationFinishesOnAnotherIdentityReplica(t *testing.T) {
	db := passkeyReplicaDB(t)
	first, _ := newPasskeyLoginServer(t)
	second, _ := newPasskeyLoginServer(t)
	first.ChallengeBackend, second.ChallengeBackend = nil, nil
	first.Store.DirectDB = func() *sql.DB { return db }
	second.Store.DirectDB = func() *sql.DB { return db }
	// Real replicas share keys. Separate fixture issuers are sufficient here:
	// each request is authenticated as the same user on its receiving server.
	bearerA := mintPasskeyBearer(t, first, passkeyTestUserId, "writer")
	bearerB := mintPasskeyBearer(t, second, passkeyTestUserId, "writer")
	a := newHTTPSoftwareAuthenticator(t)
	begin := drivePasskey(t, first, "/auth/webauthn/register/begin", bearerA,
		WebAuthnRegisterBeginRequest{}, first.handleWebAuthnRegisterBegin)
	require.Equal(t, http.StatusOK, begin.Code, begin.Body.String())
	challenge := decodeBegin(t, begin)
	finish := drivePasskey(t, second, "/auth/webauthn/register/finish", bearerB, WebAuthnRegisterFinishRequest{
		ChallengeId: challenge.ChallengeId, Label: "Replica test",
		Credential: a.create(challenge.CreationOptions.Response.Challenge.String()),
	}, second.handleWebAuthnRegisterFinish)
	require.Equal(t, http.StatusOK, finish.Code, finish.Body.String())
}

func TestPasskeyHTTPRefusesMissingSharedStorage(t *testing.T) {
	s, _ := newPasskeyLoginServer(t)
	s.ChallengeBackend = nil
	begin := beginPasskeyLogin(t, s, pkceBeginRequest())
	require.Equal(t, http.StatusServiceUnavailable, begin.Code)
	response := decodeLoginBegin(t, begin)
	require.False(t, response.Success)
	require.Empty(t, response.ChallengeId)
}

func TestPasskeyHTTPStorageOutageIsRetryableAndRecovers(t *testing.T) {
	db := passkeyReplicaDB(t)
	for _, registration := range []bool{false, true} {
		s, _ := newPasskeyLoginServer(t)
		s.ChallengeBackend = nil
		s.Store.DirectDB = func() *sql.DB { return db }
		path := "/auth/webauthn/login/finish"
		ceremony, err := s.webauthnCeremony()
		require.NoError(t, err)
		var handle, bearer string
		if registration {
			begun, err := ceremony.BeginRegistration(&webauthn.User{Id: passkeyTestUserId})
			require.NoError(t, err)
			handle = begun.ChallengeId
			bearer = mintPasskeyBearer(t, s, passkeyTestUserId, "writer")
			path = "/auth/webauthn/register/finish"
		} else {
			begun, err := ceremony.BeginLogin(webauthn.OAuthContext{})
			require.NoError(t, err)
			handle = begun.ChallengeId
		}
		s.Store.DirectDB = nil
		handler := s.handleWebAuthnLoginFinish
		if registration {
			handler = s.handleWebAuthnRegisterFinish
		}
		failed := drivePasskey(t, s, path, bearer, map[string]any{"challengeId": handle, "credential": map[string]any{}}, handler)
		require.Equal(t, http.StatusServiceUnavailable, failed.Code, failed.Body.String())
		require.Contains(t, failed.Body.String(), "temporarily unavailable")
		s.Store.DirectDB = func() *sql.DB { return db }
		kind := webauthn.CeremonyLogin
		if registration {
			kind = webauthn.CeremonyRegister
		}
		_, err = ceremony.Challenges().Take(handle, kind)
		require.NoError(t, err, "outage did not consume the shared challenge or cache a broken connection")
	}
}

type replicaDoorEngine struct{}

func (replicaDoorEngine) Execute(context.Context, string) (*memqlengine.ExecuteResult, error) {
	return memqlengine.NewResultWithOutput([]any{
		map[string]any{"reservedName": "acme.example", "status": "live"},
		map[string]any{"reservedName": "other.example", "status": "live"},
	}), nil
}

func TestAccountDoorChallengesAreSharedAndIsolated(t *testing.T) {
	db := passkeyReplicaDB(t)
	first, _ := newPasskeyLoginServer(t)
	second, _ := newPasskeyLoginServer(t)
	for _, server := range []*Server{first, second} {
		server.ChallengeBackend = nil
		server.Store.DirectDB = func() *sql.DB { return db }
		server.Doors = identity.NewDoorResolver(replicaDoorEngine{})
	}
	request := httptest.NewRequest(http.MethodPost, "https://id.acme.example/auth/webauthn/login/begin", nil)
	issuing, err := first.webauthnCeremonyFor(request)
	require.NoError(t, err)
	finishing, err := second.webauthnCeremonyFor(request)
	require.NoError(t, err)
	require.NotSame(t, issuing, finishing)
	require.Equal(t, "acme.example", issuing.RPID())
	foreign, err := second.webauthnCeremonyFor(httptest.NewRequest(http.MethodPost, "https://id.other.example/auth/webauthn/login/finish", nil))
	require.NoError(t, err)
	challenge, err := issuing.BeginLogin(webauthn.OAuthContext{ClientId: "os", State: "door-state"})
	require.NoError(t, err)
	_, err = foreign.Challenges().Take(challenge.ChallengeId, webauthn.CeremonyLogin)
	require.ErrorIs(t, err, webauthn.ErrChallengeNotFound)
	entry, err := finishing.Challenges().Take(challenge.ChallengeId, webauthn.CeremonyLogin)
	require.NoError(t, err)
	require.Equal(t, "door-state", entry.OAuth.State)
	_, err = issuing.Challenges().Take(challenge.ChallengeId, webauthn.CeremonyLogin)
	require.ErrorIs(t, err, webauthn.ErrChallengeNotFound)
}
