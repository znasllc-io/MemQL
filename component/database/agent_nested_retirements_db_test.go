package database_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAgentNestedRetirementPreservesLiveData(t *testing.T) {
	db := retiredFieldDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Shadow the real table on this connection: the migration must never
	// rewrite fixtures owned by another package running against the same DB.
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE "MemoryNodes" (id text, concept text, payload jsonb) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, concept, before, after string
	}{
		{"agent", "v1:agents:agent", `{
			"name":"kept", "avatar":{"url":"old"}, "avatarPersonaId":"old", "avatarVendor":"anam",
			"capabilities":{"avatar":false,"lipSync":true,"vision":true,"voiceToVoice":true,"claw":false,"clawWorkspace":"old","skillIds":["skill"],"keywords":["keep"],"skillBudgetMax":5},
			"providerConfig":{"voice":{"voiceId":"old"},"avatar":{"avatarId":"old"},"llm":{"policyName":"balancedChat","provider":"pin","model":"model","temperature":0.7,"maxTokens":1200}},
			"lineage":{"originatingPlanId":"old-plan","originatingRunId":"keep-run","createdBy":"system"}
		}`, `{
			"name":"kept",
			"capabilities":{"skillIds":["skill"],"keywords":["keep"],"skillBudgetMax":5},
			"providerConfig":{"llm":{"provider":"pin","model":"model","temperature":0.7,"maxTokens":1200}},
			"lineage":{"originatingRunId":"keep-run","createdBy":"system"}
		}`},
		{"missing", "v1:agents:agent", `{"name":"kept"}`, `{"name":"kept"}`},
		{"null", "v1:agents:agent", `{"capabilities":null,"providerConfig":null,"lineage":null}`, `{"capabilities":null,"providerConfig":null,"lineage":null}`},
		{"empty", "v1:agents:agent", `{"capabilities":{},"providerConfig":{"llm":{}},"lineage":{}}`, `{"capabilities":{},"providerConfig":{"llm":{}},"lineage":{}}`},
		{"role", "v1:agents:agentRole", `{"name":"kept","recommendedGender":"female"}`, `{"name":"kept"}`},
		{"event", "v1:agents:skillChangeEvent", `{"skillId":"kept","planId":"old","runId":"kept-run"}`, `{"skillId":"kept","runId":"kept-run"}`},
		{"bystander", "v1:identity:user", `{"avatar":{"url":"keep"},"avatarPersonaId":"keep","avatarVendor":"keep","recommendedGender":"keep","planId":"keep","capabilities":{"avatar":true,"clawWorkspace":"keep"},"providerConfig":{"voice":{},"avatar":{},"llm":{"policyName":"keep"}},"lineage":{"originatingPlanId":"keep"}}`, ""},
	}
	for _, tc := range tests {
		for version := 0; version < 2; version++ {
			if _, err := tx.ExecContext(ctx, `INSERT INTO "MemoryNodes" VALUES (?, ?, ?::jsonb)`, tc.name, tc.concept, tc.before); err != nil {
				t.Fatal(err)
			}
		}
	}
	sql := readMigrationSQL(t, "20260909010000_agent_nested_retirements.up.sql")
	for run := 0; run < 2; run++ {
		for _, stmt := range strings.Split(stripSQLComments(sql), ";") {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				t.Fatalf("migration run %d: %v", run, err)
			}
		}
		for _, tc := range tests {
			wantJSON := tc.after
			if wantJSON == "" {
				wantJSON = tc.before
			}
			var want any
			if err := json.Unmarshal([]byte(wantJSON), &want); err != nil {
				t.Fatal(err)
			}
			rows, err := tx.QueryContext(ctx, `SELECT payload FROM "MemoryNodes" WHERE id = ?`, tc.name)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for rows.Next() {
				var raw []byte
				if err := rows.Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var got any
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s version %d run %d: got %s, want %s", tc.name, count, run, raw, wantJSON)
				}
				count++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if count != 2 {
				t.Fatalf("%s: read %d versions, want 2", tc.name, count)
			}
		}
	}
}
