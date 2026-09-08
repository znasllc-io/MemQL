package memql

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"

	"github.com/znasllc-io/memql/component/auth"
	memqlv1 "github.com/znasllc-io/memql/component/grpc/gen"
	"github.com/znasllc-io/memql/component/memql"
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

	result := &memqlv1.MyAccessResult{
		RequestId:    requestId,
		UserId:       ac.UserId,
		PrimaryEmail: ac.PrimaryEmail,
		// From the user row the resolver already read (memql#4317) -- the
		// same read that produced PrimaryEmail, so the name costs no extra
		// query. Empty when no row resolved; a client falls back to the
		// email it is holding anyway.
		DisplayName: ac.DisplayName,
		ClusterRole: roleToProto(ac.Role),
		SessionId:   sessionIdFromClaims(ctx),
	}
	// The caller's GRANT (epic memql#5165, section H): which client groups
	// they are in, and therefore whose rows they may reach.
	//
	// Resolved from the SAME function the row gate uses, so what this reply
	// says a caller may see and what a read actually returns cannot
	// disagree. Best-effort inside the engine -- a failure answers an empty
	// grant rather than failing a reply the session does not depend on.
	applyAccessGrant(result, s.service.engine.ResolveAccessGrant(ctx))

	return s.sendServerMessage(envelope.GetMessageId(), &memqlv1.MemqlServerMessage{
		Payload: &memqlv1.MemqlServerMessage_MyAccessResult{
			MyAccessResult: result,
		},
	})
}

// roleToProto maps the auth.Role string constants to the UserRole
// proto enum.
func roleToProto(r auth.Role) memqlv1.UserRole {
	switch r {
	case auth.RoleOwner:
		return memqlv1.UserRole_USER_ROLE_OWNER
	case auth.RoleAdmin:
		return memqlv1.UserRole_USER_ROLE_ADMIN
	case auth.RoleDeveloper:
		return memqlv1.UserRole_USER_ROLE_DEVELOPER
	case auth.RoleWriter:
		return memqlv1.UserRole_USER_ROLE_WRITER
	case auth.RoleReader:
		return memqlv1.UserRole_USER_ROLE_READER
	default:
		return memqlv1.UserRole_USER_ROLE_UNSPECIFIED
	}
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

// applyAccessGrant copies the engine's answer onto the wire message.
//
// A SEPARATE FUNCTION so the mapping is testable without a stream: the
// interesting part is the staff case, where account_ids is EMPTY and empty
// means "all" rather than "none", and a mapping that silently dropped the flag
// would be indistinguishable from a caller who belongs to nothing.
func applyAccessGrant(result *memqlv1.MyAccessResult, grant memql.AccessGrant) {
	if result == nil {
		return
	}
	result.EveryAccount = grant.EveryAccount
	result.AccountIds = grant.AccountIDs
	result.Groups = make([]*memqlv1.MyAccessGroup, 0, len(grant.Groups))
	for _, g := range grant.Groups {
		result.Groups = append(result.Groups, &memqlv1.MyAccessGroup{
			Id:          g.ID,
			Name:        g.Name,
			Kind:        g.Kind,
			AccountId:   g.AccountID,
			AccountName: g.AccountName,
		})
	}
}
