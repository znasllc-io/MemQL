package airoute

// DefaultCompletionTokens is the completion budget assumed when a call
// declares none. Deliberately generous: the estimate is a FLOOR an entry must
// clear, and under-counting it admits a model that will truncate.
const DefaultCompletionTokens = 4096

// charsPerToken is the fixed estimate (design D8). It is not a tokenizer and
// is not meant to be one -- a floor that is roughly right and never zero is
// worth more here than an exact count costing a vendor round-trip. The router
// records the floor beside the chosen model's window, so a miss is
// diagnosable.
const charsPerToken = 4

// EstimateMinContextTokens returns the context-window floor for a call whose
// rendered prompt is promptText and whose completion budget is
// completionTokens (pass 0 for the default).
//
// It is NEVER zero. A zero floor admits every entry, which reads on the
// decision record exactly like a floor that was measured and cleared.
func EstimateMinContextTokens(promptText string, completionTokens int) int {
	if completionTokens <= 0 {
		completionTokens = DefaultCompletionTokens
	}
	prompt := (len(promptText) + charsPerToken - 1) / charsPerToken
	if prompt < 1 {
		prompt = 1
	}
	return prompt + completionTokens
}

// EstimateMinContextTokensFor sums several rendered parts (a system prompt, a
// history, a user turn) before applying the same floor. Callers with more than
// one string use it rather than concatenating, which would allocate the whole
// conversation to count it.
func EstimateMinContextTokensFor(completionTokens int, parts ...string) int {
	chars := 0
	for _, p := range parts {
		chars += len(p)
	}
	if completionTokens <= 0 {
		completionTokens = DefaultCompletionTokens
	}
	prompt := (chars + charsPerToken - 1) / charsPerToken
	if prompt < 1 {
		prompt = 1
	}
	return prompt + completionTokens
}
