package worker

import "strings"

// SHARING: two consents, and neither one alone (epic memql#5146, design D6).
//
// ===========================================================================
// THE PROBLEM THIS SOLVES
// ===========================================================================
// A fleet machine serves only its owner's calls. A business with one Mac Studio
// in the office has no way to make it serve the team, so "local by default" is
// per PERSON rather than per COMPANY -- and the cost principle this platform is
// built on applies to an individual instead of to the business whose logic it is
// meant to hand back to them.
//
// ===========================================================================
// WHY TWO CONSENTS AND NOT ONE FLAG
// ===========================================================================
// The owner's consent is a decision about their property. The cockpit's is a
// decision about the machine's SITUATION: it may be a laptop on a train, a
// build box under somebody's desk, a machine in a room where running strangers'
// prompts is not allowed. Those are different questions and different people
// answer them, so a single flag would let either one speak for the other.
//
// It is the `allowed` and `signedIn` shape the local-app door already uses, and
// the reasoning is the same: the machine's own policy.yaml is the only thing
// that knows what the machine is for.
//
// ===========================================================================
// THE REFUSAL NAMES WHICH HALF IS MISSING
// ===========================================================================
// The two repairs are in different places -- the owner's is one act on the
// Fleet page, the cockpit's is a line in that machine's policy.yaml -- so a
// single "not shared" sentence sends half the operators to the wrong machine.
// SharingRefusal is what stops that being a UI decision.

// SharingMode values on registration.sharing.mode.
const (
	// SharingModeOwner is the default and the absent case: this machine serves
	// its owner's calls and nobody else's.
	SharingModeOwner = "owner"
	// SharingModeCluster is the owner offering the machine to everyone.
	SharingModeCluster = "cluster"
)

// Sharing is the owner's half, as stored on the registration row.
type Sharing struct {
	Mode     string
	SharedAt string
	SharedBy string
}

// SharingFromRow reads registration.sharing.
//
// A MISSING KEY IS `owner`, and here the safe reading and the honest reading
// coincide: a row written before the field existed carries no consent, and no
// consent is not consent.
func SharingFromRow(v any) Sharing {
	row, ok := v.(map[string]any)
	if !ok || len(row) == 0 {
		return Sharing{Mode: SharingModeOwner}
	}
	mode, _ := row["mode"].(string)
	mode = strings.TrimSpace(mode)
	if mode != SharingModeCluster {
		// Anything that is not exactly `cluster` is `owner`. A typo, a value
		// from a future engine, a half-written row: all of them are read as
		// NOT shared, because the failure direction here is somebody's laptop
		// running a stranger's prompt.
		mode = SharingModeOwner
	}
	sharedAt, _ := row["sharedAt"].(string)
	sharedBy, _ := row["sharedBy"].(string)
	return Sharing{Mode: mode, SharedAt: sharedAt, SharedBy: sharedBy}
}

// Row renders the owner's consent for storage.
func (s Sharing) Row() map[string]any {
	mode := s.Mode
	if mode != SharingModeCluster {
		mode = SharingModeOwner
	}
	return map[string]any{"mode": mode, "sharedAt": s.SharedAt, "sharedBy": s.SharedBy}
}

// ServesTheCluster reports whether BOTH consents say cluster.
//
// `cockpitServe` is capabilityDescriptor.inferenceServe, and an EMPTY value is
// `owner`: a cockpit that predates the field has said nothing, and silence is
// not agreement.
func ServesTheCluster(ownerMode, cockpitServe string) bool {
	return strings.TrimSpace(ownerMode) == SharingModeCluster &&
		strings.TrimSpace(cockpitServe) == InferenceServeCluster
}

// SharingRefusal names WHICH consent is missing, in the words of the repair.
//
// Empty when the machine does serve the cluster. The two sentences are
// different because the two fixes are in different places and are performed by
// different people; a single "not shared" would send half the operators to the
// wrong machine, and the one who owns the laptop would go looking on a web page
// for a setting that lives in a file on their own disk.
func SharingRefusal(ownerMode, cockpitServe string) string {
	ownerSaid := strings.TrimSpace(ownerMode) == SharingModeCluster
	cockpitSaid := strings.TrimSpace(cockpitServe) == InferenceServeCluster
	switch {
	case ownerSaid && cockpitSaid:
		return ""
	case !ownerSaid && !cockpitSaid:
		return "Neither consent is given: the owner has not shared this machine with the cluster, and its cockpit's policy.yaml does not set inference.serve to cluster. Both are needed."
	case !ownerSaid:
		return "The machine's cockpit is willing to serve the cluster, but its owner has not shared it. The owner turns this on from the machine's page in Fleet."
	default:
		return "The owner has shared this machine, but its cockpit is not willing to serve the cluster. Set inference.serve to cluster in that machine's policy.yaml -- it is a decision about where the machine is, and only the machine can make it."
	}
}
