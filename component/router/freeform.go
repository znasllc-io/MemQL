package router

import (
	"path"
	"strings"
)

// Classifying a FREE-FORM request (epic memql#5137, D9).
//
// ===========================================================================
// WHY ONLY TWO PLACES NEED THIS
// ===========================================================================
// Every other call site in the platform is a PROMPT, and a prompt construct
// already says what the call is for -- its `@level` is a declaration, not a
// guess. Twenty-eight of the thirty sites are in that position. The Ask surface
// and an agent turn are the two where nobody declared anything, because the
// request is whatever a person typed.
//
// ===========================================================================
// RULES FIRST, AND THE ORDER IS THE COST ARGUMENT
// ===========================================================================
// A model in front of every model call is exactly the cost this whole program
// exists to avoid. So a small table answers first -- an image verb, an audio
// verb, an attached file's kind, a tool the person already chose -- and only a
// MISS reaches `classifyRequest` at level fast. On a cache hit, not even that.
//
// The table is a TABLE rather than a chain of ifs on purpose: the order inside
// it is load-bearing (a request naming a file beats a request naming a verb,
// because the file is a fact and the verb is a guess about intent), and an
// order that lives in control flow is one nobody can read.

// Modality is what a free-form request turns out to be about.
type Modality string

const (
	ModalityText      Modality = "text"
	ModalityVision    Modality = "vision"
	ModalityAudioIn   Modality = "audioIn"
	ModalityAudioOut  Modality = "audioOut"
	ModalityImageGen  Modality = "imageGen"
	ModalityEmbedding Modality = "embeddings"
)

// FreeFormRequest is what the two undeclared surfaces know about a request.
type FreeFormRequest struct {
	// Text is what the person typed.
	Text string
	// AttachmentTypes are the IANA types of anything attached, e.g.
	// "image/png", "audio/wav". A FACT rather than an inference, which is why
	// it outranks the verb rules below.
	AttachmentTypes []string
	// ChosenTool is a tool the person selected explicitly. Also a fact.
	ChosenTool string
}

// Classification is the verdict.
type Classification struct {
	Modality Modality
	// Level is one of the four: fast, strong, reasoning, embeddings.
	Level string
	// DecidedBy names the rule that answered, or "classifier" when the model
	// did. NEVER EMPTY on a returned verdict.
	//
	// It is what makes a misfiling debuggable: a decision record saying
	// "attachment:image" is a different problem from one saying "verb:draw",
	// and both are different from the classifier having guessed.
	DecidedBy string
}

// freeFormRule is one row of the table.
type freeFormRule struct {
	name  string
	match func(FreeFormRequest) bool
	give  Classification
}

// imageVerbs and audioVerbs are deliberately SHORT.
//
// A long list of synonyms is a worse classifier than a short list plus a model:
// it grows until somebody's ordinary sentence trips it, and then an image
// model answers a question about painting. These are the cases where the verb
// is unambiguous and the phrasing is the common one; everything else is a miss,
// which costs one cheap local call and gets it right.
var imageVerbs = []string{"draw ", "generate an image", "generate a picture", "make an image", "make a picture", "paint "}

var audioOutVerbs = []string{"read this aloud", "say this out loud", "speak this", "text to speech"}

// hasAttachmentOfType reports whether any attachment's type starts with the
// given prefix ("image/", "audio/").
func hasAttachmentOfType(req FreeFormRequest, prefix string) bool {
	for _, t := range req.AttachmentTypes {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), prefix) {
			return true
		}
	}
	return false
}

func containsAny(text string, needles []string) bool {
	lower := strings.ToLower(text)
	for _, n := range needles {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// freeFormRules is the table, IN PRIORITY ORDER.
//
// FACTS BEFORE GUESSES. An attached image is a fact about the request; "draw"
// appearing in a sentence is a guess about intent, and the guess is wrong for
// "what does this diagram draw attention to". So attachments and an explicitly
// chosen tool come first, and the verb rules only ever fire on a request that
// carried neither.
var freeFormRules = []freeFormRule{
	{
		name:  "attachment:image",
		match: func(r FreeFormRequest) bool { return hasAttachmentOfType(r, "image/") },
		give:  Classification{Modality: ModalityVision, Level: "fast", DecidedBy: "attachment:image"},
	},
	{
		name:  "attachment:audio",
		match: func(r FreeFormRequest) bool { return hasAttachmentOfType(r, "audio/") },
		give:  Classification{Modality: ModalityAudioIn, Level: "fast", DecidedBy: "attachment:audio"},
	},
	{
		name:  "tool:chosen",
		match: func(r FreeFormRequest) bool { return strings.TrimSpace(r.ChosenTool) != "" },
		// A CHOSEN TOOL IS A FACT AND STILL LEAVES THE MODALITY TEXT: the person
		// picked a tool, not a picture. What it settles is that this is a tool
		// turn, which needs a model that can carry one -- `strong` rather than
		// `fast`, because a tool turn that picks the wrong function is worse
		// than a slow one.
		give: Classification{Modality: ModalityText, Level: "strong", DecidedBy: "tool:chosen"},
	},
	{
		name:  "verb:image",
		match: func(r FreeFormRequest) bool { return containsAny(r.Text, imageVerbs) },
		give:  Classification{Modality: ModalityImageGen, Level: "fast", DecidedBy: "verb:image"},
	},
	{
		name:  "verb:speak",
		match: func(r FreeFormRequest) bool { return containsAny(r.Text, audioOutVerbs) },
		give:  Classification{Modality: ModalityAudioOut, Level: "fast", DecidedBy: "verb:speak"},
	},
}

// ClassifyByRules answers from the table, or reports that it could not.
//
// THE SECOND RETURN IS THE WHOLE POINT: false is what costs a model call, so a
// caller counting calls counts misses. A version of this that always returned a
// verdict would be indistinguishable from one that had stopped working.
func ClassifyByRules(req FreeFormRequest) (Classification, bool) {
	for _, rule := range freeFormRules {
		if rule.match(req) {
			return rule.give, true
		}
	}
	return Classification{}, false
}

// FreeFormCacheNamespace is where a classifier verdict is cached.
//
// IT CARRIES THE RULE-SET HASH, and that is the invalidation mechanism (D8).
// A verdict is only as good as the rules that failed to answer before it, so
// when the rules change every cached verdict must stop being served -- and
// putting the hash in the NAMESPACE does that atomically, with no sweep to run
// and nothing to forget. The old namespace simply stops being read.
//
// A clock would be the wrong instrument: nothing about a verdict decays with
// time, and a TTL long enough to be useful is long enough to serve a verdict
// the current rules would not give.
func FreeFormCacheNamespace(ruleSetHash string) string {
	h := strings.TrimSpace(ruleSetHash)
	if h == "" {
		// An empty hash would collapse every rule set onto one namespace, which
		// is the failure this function exists to prevent. Naming it explicitly
		// keeps a caller that forgot to hash from silently sharing verdicts.
		h = "unhashed"
	}
	return path.Join("router.freeform", h)
}

// RuleSetHashInput is the string a caller hashes to get the namespace suffix.
//
// It is built from the rule NAMES in order, so adding, removing or reordering a
// rule changes it -- and reordering matters, because the table's order decides
// which of two matching rules answers.
func RuleSetHashInput() string {
	names := make([]string, 0, len(freeFormRules))
	for _, r := range freeFormRules {
		names = append(names, r.name)
	}
	return strings.Join(names, "|")
}
