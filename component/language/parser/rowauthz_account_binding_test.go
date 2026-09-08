package parser

import (
	"strings"
	"testing"
)

// The account grant on the owned tier (epic memql#5165, task memql#5169).
//
// These sit beside the rank modifiers' own tests for the reason those sit
// beside the composite tier's: `account=` is one more ARGUMENT of the owned
// tier, not a tier, so every existing declaration must keep parsing to
// exactly what it parsed to before -- and the new one must compose with all
// of them rather than replacing any.

func TestParseRowAuthzAccountArgument(t *testing.T) {
	cases := []struct {
		name string
		line string
		want RowAuthzDecl
	}{
		{
			"a scalar account field",
			`@rowAuthz(owner="ownerUserId", account="accountId")`,
			RowAuthzDecl{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountId"},
		},
		{
			// A list field is the SAME argument, deliberately: "which
			// accounts is this row for" is one question, and a second
			// spelling would be a second thing an author has to get right
			// about a decision the concept has already made. The lowering
			// reads the concept's declared type, not the annotation.
			"a list account field",
			`@rowAuthz(owner="ownerUserId", account="accountIds")`,
			RowAuthzDecl{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountIds"},
		},
		{
			"beside the cluster-owner escape",
			`@rowAuthz(owner="ownerUserId", account="accountId", clusterOwner)`,
			RowAuthzDecl{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountId", ClusterOwnerBypass: true},
		},
		{
			"beside every rank modifier",
			`@rowAuthz(owner="ownerUserId", rankVisible, rankStrict, unowned="admin", account="accountId", clusterOwner)`,
			RowAuthzDecl{
				Tier: RowAuthzOwned, Owner: "ownerUserId",
				RankVisible: true, RankStrict: true, Unowned: "admin",
				Account: "accountId", ClusterOwnerBypass: true,
			},
		},
		{
			// An attribute's argument list is a MAP, so there is no order
			// to depend on -- the property the rank modifiers already pin.
			"argument order does not matter",
			`@rowAuthz(account="accountId", clusterOwner, owner="ownerUserId")`,
			RowAuthzDecl{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountId", ClusterOwnerBypass: true},
		},
		{
			// The positive control's negative twin: a declaration WITHOUT
			// the argument must still parse to a decl whose Account is
			// empty, or every test above is measuring a field that is
			// always set.
			"the composite tier is unchanged",
			`@rowAuthz(owner="ownerUserId", clusterOwner)`,
			RowAuthzDecl{Tier: RowAuthzOwned, Owner: "ownerUserId", ClusterOwnerBypass: true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRowAuthz(rowAuthzAttr(t, tc.line))
			if err != nil {
				t.Fatalf("ParseRowAuthz(%s) error = %v", tc.line, err)
			}
			if *got != tc.want {
				t.Fatalf("ParseRowAuthz(%s) = %+v, want %+v", tc.line, *got, tc.want)
			}
		})
	}
}

// The refusals. Each names the argument that is wrong AND what is accepted:
// an author one word away from a legal security declaration should not have
// to guess which word.
func TestParseRowAuthzRefusesIncoherentAccountDeclarations(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantNames string
	}{
		{
			// `account=` is an argument of the OWNED tier. Without an
			// `owner=` there is no tier for it to qualify.
			//
			// The DIAGNOSTIC changed in memql#5216 and the refusal did not.
			// This used to fall through to the shared "takes exactly one tier"
			// message, which was a fallthrough artifact rather than a
			// description -- only one tier is named in this input. Now that the
			// cluster-owner tier has a modifier of its own the shape is
			// recognised, so the message can say the true thing and still list
			// every accepted form, which is the property asserted below.
			"account with no owner field",
			`@rowAuthz(clusterOwner, account="accountId")`,
			"the cluster-owner tier takes",
		},
		{
			"account on the granted tier",
			`@rowAuthz(via="spaceMember", account="accountId")`,
			"exactly one tier",
		},
		{
			"account with no field name",
			`@rowAuthz(owner="ownerUserId", account="")`,
			"quoted field name",
		},
		{
			"account written as a bare flag",
			`@rowAuthz(owner="ownerUserId", account)`,
			"quoted field name",
		},
		{
			// The one genuinely ambiguous pairing. Declared on the owner
			// field the argument compares a user id against an account id
			// and lowers to a scope matching nothing -- a gate that reads
			// like a widening and grants nobody anything.
			"account naming the owner field",
			`@rowAuthz(owner="ownerUserId", account="ownerUserId")`,
			"never the person who owns it",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decl, err := ParseRowAuthz(rowAuthzAttr(t, tc.line))
			if err == nil {
				t.Fatalf("ParseRowAuthz(%s): want a refusal, got %+v", tc.line, decl)
			}
			if !strings.Contains(err.Error(), tc.wantNames) {
				t.Fatalf("ParseRowAuthz(%s) error = %q, want it to name %q", tc.line, err, tc.wantNames)
			}
			// The shared property every refusal in this family carries: say
			// what IS accepted, or the author is guessing at a security
			// declaration.
			if !strings.Contains(err.Error(), `owner="<field>"`) {
				t.Fatalf("ParseRowAuthz(%s) error = %q, want it to name the accepted forms", tc.line, err)
			}
		})
	}
}

// A blank `unowned=` must still say "role slug" and a blank `account=` must
// say "field name". The nouns are not cosmetic: telling an author who wrote
// `account=` that a role slug was wanted sends them to the role catalog to
// fix a concept.
func TestParseRowAuthzKeywordRefusalsNameTheRightNoun(t *testing.T) {
	for _, tc := range []struct {
		line      string
		wantNoun  string
		wantPlace string
	}{
		{`@rowAuthz(owner="ownerUserId", rankVisible, unowned="")`, "quoted role slug", `unowned="developer"`},
		{`@rowAuthz(owner="ownerUserId", account="")`, "quoted field name", `account="accountId"`},
	} {
		_, err := ParseRowAuthz(rowAuthzAttr(t, tc.line))
		if err == nil {
			t.Fatalf("ParseRowAuthz(%s): want a refusal", tc.line)
		}
		if !strings.Contains(err.Error(), tc.wantNoun) {
			t.Fatalf("ParseRowAuthz(%s) error = %q, want noun %q", tc.line, err, tc.wantNoun)
		}
		if !strings.Contains(err.Error(), tc.wantPlace) {
			t.Fatalf("ParseRowAuthz(%s) error = %q, want placeholder %q", tc.line, err, tc.wantPlace)
		}
	}
}

// FormatRowAuthz is the only renderer, so anything it writes must read back
// as the SAME decl -- otherwise the codemod can silently rewrite one
// authorization statement into another.
func TestFormatRowAuthzRoundTripsTheAccountArgument(t *testing.T) {
	for _, d := range []RowAuthzDecl{
		{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountId"},
		{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountIds"},
		{Tier: RowAuthzOwned, Owner: "ownerUserId", Account: "accountId", ClusterOwnerBypass: true},
		{Tier: RowAuthzOwned, Owner: "ownerUserId", RankVisible: true, Account: "accountId"},
		{
			Tier: RowAuthzOwned, Owner: "ownerUserId",
			RankVisible: true, RankStrict: true, Unowned: "admin",
			Account: "accountId", ClusterOwnerBypass: true,
		},
	} {
		rendered, err := FormatRowAuthz(d)
		if err != nil {
			t.Fatalf("FormatRowAuthz(%+v) error = %v", d, err)
		}
		back, err := ParseRowAuthz(rowAuthzAttr(t, rendered))
		if err != nil {
			t.Fatalf("FormatRowAuthz(%+v) rendered %s, which does not parse: %v", d, rendered, err)
		}
		if *back != d {
			t.Fatalf("round trip: %+v -> %s -> %+v", d, rendered, *back)
		}
	}
}

// The negative control for the round trip: a renderer that DROPS the
// argument emits something the parser reads back as a weaker declaration,
// with nothing failing.
func TestFormatRowAuthzRefusesAnAccountItCannotRoundTrip(t *testing.T) {
	for _, d := range []RowAuthzDecl{
		{Tier: RowAuthzClusterOwner, Account: "accountId"},
		{Tier: RowAuthzPublic, Account: "accountId"},
		{Tier: RowAuthzGranted, Spec: "spaceMember", Account: "accountId"},
		{Tier: RowAuthzOwned, Owner: "accountId", Account: "accountId"},
	} {
		if rendered, err := FormatRowAuthz(d); err == nil {
			t.Fatalf("FormatRowAuthz(%+v) = %q, want a refusal", d, rendered)
		}
	}
}
