package email

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/znasllc-io/memql/component/memql/readiness"
)

// Run the real status capability through the decoder used by module readiness.
// Keeping this test in the integration preserves the module dependency direction.
func TestEmailStatusEnvelopeReadinessContract(t *testing.T) {
	for _, tc := range []struct {
		name                string
		configured, partial bool
		want                string
		touched             bool
	}{
		{"configured", true, false, StateConfigured, true},
		{"partial", false, true, StateNeedsConfiguration, true},
		{"empty", false, false, StateNeedsConfiguration, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEmailEnv(t)
			keys := DefaultGraphEnvKeys()
			if tc.configured || tc.partial {
				t.Setenv(keys.TenantId, "fixture-tenant")
			}
			if tc.configured {
				t.Setenv(keys.ClientId, "fixture-client")
				t.Setenv(keys.ClientSecret, plantedGraphSecret)
				t.Setenv(keys.SenderAddr, "sender@example.invalid")
				t.Setenv(keys.FromName, "Fixture")
			}
			integration := integrationWith(nil, nil)
			for _, capability := range integration.Capabilities() {
				if capability.Name == "status" {
					nodes, err := capability.Handler(ownerContext(), map[string]any{"probe": false}, 0)
					if err != nil {
						t.Fatal(err)
					}
					if len(nodes) != 1 {
						t.Fatalf("status nodes: %d", len(nodes))
					}
					state, touched, err := readiness.IntegrationStatus(nodes[0].Payload, "email")
					if err != nil {
						t.Fatal(err)
					}
					if state != tc.want || touched != tc.touched {
						t.Fatalf("state=%q touched=%t, want %q/%t", state, touched, tc.want, tc.touched)
					}
					encoded, _ := json.Marshal([]any{state, touched})
					if strings.Contains(string(encoded), plantedGraphSecret) {
						t.Fatal("readiness leaked credential")
					}
					return
				}
			}
			t.Fatal("status capability missing")
		})
	}
}
