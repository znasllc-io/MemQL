package readiness

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

const osFoldPath = "../../../clients/os/src/system/readinessFold.ts"

var windowPattern = regexp.MustCompile(`(?m)^\s*export\s+const\s+NODE_LIVE_WINDOW_SECONDS\s*=\s*(\d+)\s*;`)

// The live window is a number both sides must agree on, and disagreement is
// silent: the shell would count a node the engine had written off, or write
// off one the engine still counts, and the two surfaces would answer
// differently about the same cluster with neither logging anything.
//
// The same arrangement TestFleetOnlineWindowMatchesTheClients uses for the
// online window, for the same reason.
func TestNodeLiveWindowMatchesTheClient(t *testing.T) {
	raw, err := os.ReadFile(osFoldPath)
	if err != nil {
		t.Fatalf("the shell's fold is unreadable at %s: %v", osFoldPath, err)
	}
	m := windowPattern.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("%s does not export NODE_LIVE_WINDOW_SECONDS as a numeric literal; this gate reads it by regexp rather than executing TypeScript, so the shape is load-bearing", osFoldPath)
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	if want := int(NodeLiveWindow.Seconds()); got != want {
		t.Fatalf("NODE_LIVE_WINDOW_SECONDS is %d in the shell and %d in Go", got, want)
	}
}
