package memql_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	memorynodes "github.com/znasllc-io/memql/component/database/memory-nodes"
	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/component/memql/readiness"
	"github.com/znasllc-io/memql/integrations/email"
)

func TestEmailStatusEnvelopeReadinessContract(t *testing.T) {
	for _, tc := range []struct {
		name                string
		configured, partial bool
		want                readiness.State
	}{
		{"configured", true, false, readiness.Configured},
		{"partial", false, true, readiness.Partial},
		{"empty", false, false, readiness.Unconfigured},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, keys := range []email.GraphEnvKeys{email.DefaultGraphEnvKeys(), email.LegacyGraphEnvKeys()} {
				for _, key := range []string{keys.TenantId, keys.ClientId, keys.ClientSecret, keys.SenderAddr, keys.FromName} {
					t.Setenv(key, "")
				}
			}
			smtp := email.DefaultEnvKeys()
			for _, key := range []string{smtp.Host, smtp.Port, smtp.Username, smtp.Password, smtp.FromAddr, smtp.FromName, email.DomainEnv, email.AllowLogOnlyEnv} {
				t.Setenv(key, "")
			}
			keys := email.DefaultGraphEnvKeys()
			if tc.configured || tc.partial {
				t.Setenv(keys.TenantId, "fixture-tenant")
			}
			const secret = "READINESS-FIXTURE-SECRET-NEVER-EMITTED"
			if tc.configured {
				t.Setenv(keys.ClientId, "fixture-client")
				t.Setenv(keys.ClientSecret, secret)
				t.Setenv(keys.SenderAddr, "sender@example.invalid")
				t.Setenv(keys.FromName, "Fixture")
			}
			resolver := func(context.Context, string) (string, error) { return "", nil }
			integration := email.NewIntegration(email.NewLazySender(email.NewLogSender(nil), resolver, email.SecretResolver(resolver), nil), nil)
			called := false
			for _, capability := range integration.Capabilities() {
				if capability.Name == "status" {
					real := capability.Handler
					capability.Handler = func(ctx context.Context, args map[string]any, depth int) ([]memorynodes.MemoryNode, error) {
						called = true
						if args["probe"] != false {
							t.Fatal("readiness must not perform network probes")
						}
						return real(ctx, args, depth)
					}
					report := memql.EvaluateIntegrationReadinessForTest("email", capability)
					if report.State != tc.want {
						t.Errorf("actual email status -> readiness = %s, want %s", report.State, tc.want)
					}
					encoded, err := json.Marshal(report)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(encoded), secret) {
						t.Fatal("readiness leaked fixture credential")
					}
				}
			}
			if !called {
				t.Fatal("real status handler was not called")
			}
		})
	}
}

func TestIntegrationReadinessEnvelopeSelection(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		want          readiness.State
	}{
		{"named report", `{"integrations":[{"name":"other","state":"configured"},{"name":"email","state":"needs_configuration"}]}`, readiness.Unconfigured},
		{"configured", `{"integrations":[{"name":"email","state":"configured"}]}`, readiness.Configured},
		{"unhealthy remains configured", `{"integrations":[{"name":"email","state":"unhealthy"}]}`, readiness.Configured},
		{"credential touched", `{"integrations":[{"name":"email","state":"needs_configuration","credentials":[{"present":true}]}]}`, readiness.Partial},
		{"unknown state", `{"integrations":[{"name":"email","state":"future_state","settings":[{"source":"env"}]}]}`, readiness.NotApplicable},
		{"empty state", `{"integrations":[{"name":"email","state":""}]}`, readiness.NotApplicable},
		{"missing named report", `{"integrations":[{"name":"other","state":"configured"}]}`, readiness.NotApplicable},
		{"malformed", `{"integrations":`, readiness.NotApplicable},
		{"wrong shape", `{"integrations":{}}`, readiness.NotApplicable},
		{"null", `null`, readiness.NotApplicable},
		{"root state is not a report", `{"state":"configured"}`, readiness.NotApplicable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := memql.IntegrationCapability{Handler: func(context.Context, map[string]any, int) ([]memorynodes.MemoryNode, error) {
				return []memorynodes.MemoryNode{{Payload: []byte(tc.payload)}}, nil
			}}
			if got := memql.EvaluateIntegrationReadinessForTest("email", cap).State; got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	t.Run("absent capability", func(t *testing.T) {
		if got := memql.EvaluateIntegrationReadinessForTest("email", memql.IntegrationCapability{}).State; got != readiness.NotApplicable {
			t.Fatal(got)
		}
	})
	t.Run("handler failure", func(t *testing.T) {
		cap := memql.IntegrationCapability{Handler: func(context.Context, map[string]any, int) ([]memorynodes.MemoryNode, error) {
			return nil, errors.New("fixture failure")
		}}
		if got := memql.EvaluateIntegrationReadinessForTest("email", cap).State; got != readiness.NotApplicable {
			t.Fatal(got)
		}
	})
}
