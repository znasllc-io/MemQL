package memql

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"

	"github.com/znasllc-io/memql/component/auth"
	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
)

// handleMyAccess returns the caller's own identity record: userId,
// email, cluster-wide role. Used by the Cockpit Settings tab's
// "My Access" section.
//
// The PartitionGrants field on the wire is kept as an empty list for
// proto compatibility until the partition wire dimension is dropped
// in #56 phase 8.
func (s *streamSession) handleMyAccess(envelope *memqlv1.MemqlClientMessage, msg *memqlv1.MyAccessMsg) error {
	requestId := ""
	if msg != nil {
		requestId = msg.GetRequestId()
	}

	ctx := s.stream.Context()
	ac := s.ensureAccess(ctx)
	if ac == nil {
		return s.sendQueryError(requestId, envelope.GetMessageId(), codes.Unauthenticated, "access context not available")
	}

	slug := strings.ToLower(strings.TrimSpace(string(ac.Role)))
	result := &memqlv1.MyAccessResult{
		RequestId:    requestId,
		UserId:       ac.UserId,
		PrimaryEmail: ac.PrimaryEmail,
		// From the user row the resolver already read (memql#4317) -- the
		// same read that produced PrimaryEmail, so the name costs no extra
		// query. Empty when no row resolved; a client falls back to the
		// email it is holding anyway.
		DisplayName: ac.DisplayName,
		SessionId:   sessionIdFromClaims(ctx),
		// The role as a SLUG (epic memql#5166, D12). It was the UserRole enum,
		// which could name only the roles this repo shipped -- a cluster's own
		// role reported USER_ROLE_UNSPECIFIED, the value an unauthenticated
		// caller gets, so a shell could not tell "you hold a custom role" from
		// "you hold none".
		Role: slug,
	}
	// THE NAME AND THE RANK COME FROM THE CATALOG, and their absence is a real
	// answer rather than a failure. A slug the catalog does not carry -- a role
	// deactivated under its holder, a node whose rows have not loaded -- leaves
	// role_name empty and rank 0, and the client renders the slug it already
	// holds. Inventing a title for a role the cluster does not recognise is the
	// one thing this must not do: it would tell somebody they hold a role the
	// engine will refuse them everything for.
	if cat := auth.InstalledCapabilityCatalog(); cat != nil {
		if rank, ok := cat.Rank(slug); ok {
			result.Rank = int32(rank)
			result.RoleName = cat.Name(slug)
		}
	}

	return s.sendServerMessage(envelope.GetMessageId(), &memqlv1.MemqlServerMessage{
		Payload: &memqlv1.MemqlServerMessage_MyAccessResult{
			MyAccessResult: result,
		},
	})
}

// sessionIdFromClaims reads the `sid` claim off the VERIFIED token
// (memql#4306).
//
// Verified is the operative word: these claims were attached by the identity
// verifier after checking the signature, so this is the server reporting what
// it already believes rather than trusting anything the client said. That is
// the whole reason the field exists -- the portal refuses to decode JWTs by
// standing rule, and a client that parsed its own bearer to find the session
// id would be making decisions from claims nobody promised it.
//
// Empty for a credential that carries no session -- a PAT, an operator key, a
// service-account token. Not an error: those bearers have no session row to
// name, and a client marking "this device" simply marks nothing.
func sessionIdFromClaims(ctx context.Context) string {
	claims, ok := auth.ClaimsFromContext(ctx)
	if !ok {
		return ""
	}
	sid, _ := claims["sid"].(string)
	return strings.TrimSpace(sid)
}
