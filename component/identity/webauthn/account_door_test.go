package webauthn

import "testing"

// The relying party for an account's reserved front door (epic memql#5168,
// design G).
//
// These live HERE rather than beside the DoorResolver that feeds them because
// component/identity imports this package -- an in-package test there cannot
// import it back.

// ===========================================================================
// The relying party
// ===========================================================================

// ONE PASSKEY ACROSS BOTH HOSTS. The RP id is the reserved name rather than
// the id. host, because an RP id may be a registrable-domain suffix of the
// origin -- scoping it to id. would mint a credential the OS could never use,
// and WebAuthn credentials cannot be re-scoped afterwards.
func TestTheDoorsRelyingPartyIsTheReservedName(t *testing.T) {
	rpID, origin, err := RelyingPartyForDoor("memql.acme.com")
	if err != nil {
		t.Fatalf("RelyingPartyForDoor: %v", err)
	}
	if rpID != "memql.acme.com" {
		t.Errorf("rpID = %q, want the reserved name so one passkey works on app. and id. alike", rpID)
	}
	if origin != "https://id.memql.acme.com" {
		t.Errorf("origin = %q, want the id. host the ceremony runs on", origin)
	}
}

func TestARelyingPartyRefusesAnUnusableName(t *testing.T) {
	for _, name := range []string{"", "   ", "localhost", "acme"} {
		if _, _, err := RelyingPartyForDoor(name); err == nil {
			t.Errorf("RelyingPartyForDoor(%q) returned no error; a guessed RP id mints credentials scoped to the wrong place", name)
		}
	}
}
