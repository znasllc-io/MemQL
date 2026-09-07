package memql

import (
	"log/slog"
	"os"
	"testing"

	memoryNodes "github.com/znasllc-io/memql/component/database/memory-nodes"
)

// Test #1630: for several mutations the reflected tool argsSchema declared
// a field as one JSON type while the bound concept required another, so NO
// input satisfied both -- the typed-create / typed-update path was
// unsatisfiable via the MCP connector and the agent tool loop.
//
// Affected (all array concept fields mis-declared as `object` in the
// mutation args block, plus the delegation payload/field mismatches):
//   - recordLegalAcceptance.legalAcceptance  (user.legalAcceptance []object)
//   - mutationCreateDomainEntitySchema.keyFields/displayFields (domainEntitySchema []string)
//   - createDelegation.scopes (delegation.scopes []string),
//     roleCeiling enum, missing createdBySubject, spurious agentSubject.
//
// This loads the real DSL, reflects each mutation's tool InputSchema the
// same way registerFunctionTools does, and validates the CONCEPT-VALID
// shape against it. Before the DSL fix these mutations declared `object`
// args, so an array (the concept-valid value) fails validation; after the
// fix the array validates.
func TestToolSchema1630_ArgConceptTypesReconciled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	if _, err := LoadUnifiedConcepts(logger); err != nil {
		t.Fatalf("LoadUnifiedConcepts: %v", err)
	}
	registry := newFunctionRegistry()
	if _, _, err := LoadUnifiedFunctions(logger, registry, memoryNodes.DefaultRegistry()); err != nil {
		t.Fatalf("LoadUnifiedFunctions: %v", err)
	}
	snapshot := registry.LookupIndex()

	schemaFor := func(t *testing.T, mutation string) interface {
		Validate(any) error
	} {
		t.Helper()
		fn := snapshot[mutation]
		if fn == nil {
			t.Fatalf("mutation %q not found in function registry", mutation)
		}
		raw, err := toolInputSchemaFromArgs(fn.ArgsSchema)
		if err != nil {
			t.Fatalf("%s: toolInputSchemaFromArgs: %v", mutation, err)
		}
		return compileSchema(t, raw)
	}

	t.Run("recordLegalAcceptance accepts legalAcceptance array", func(t *testing.T) {
		s := schemaFor(t, "recordLegalAcceptance")
		args := map[string]any{
			"userId": "v1:identity:user:abc",
			"legalAcceptance": []any{
				map[string]any{"documentType": "tos", "version": "1.0", "acceptedAt": "2026-01-01T00:00:00Z", "sourceIP": "1.2.3.4"},
			},
		}
		if err := s.Validate(args); err != nil {
			t.Fatalf("concept-valid legalAcceptance array rejected: %v", err)
		}
	})

	// The persistTaskState subtest that stood here went with v1:planner:taskState
	// in memql#5053. It asserted the SAME property the other subtests assert --
	// a concept-valid ARRAY validates against the mutation's reflected tool
	// schema, which fails when the DSL declares the arg as `object` -- and
	// that property is still covered three times over below and above. Nothing
	// was re-pointed because nothing was lost.

	t.Run("createDelegation typed-create is self-consistent", func(t *testing.T) {
		s := schemaFor(t, "createDelegation")
		// scopes is an array (concept []string), roleCeiling is a
		// concept-enum value, createdBySubject is present, and
		// agentSubject is NOT a field (it is not on the concept and was
		// rejected by additionalProperties:false in the payload).
		args := map[string]any{
			"identityId":       "v1:identity:identity:abc",
			"identitySubject":  "user:abc",
			"identityType":     "human",
			"agentId":          "v1:agents:agent:xyz",
			"roleCeiling":      "writer",
			"scopes":           []any{"query:*", "mutation:cognition.*"},
			"createdBySubject": "user:abc",
		}
		if err := s.Validate(args); err != nil {
			t.Fatalf("concept-valid delegation create rejected: %v", err)
		}

		// roleCeiling must reject a value outside the concept enum
		// (the old arg had no enum and accepted "member").
		bad := map[string]any{
			"identityId":       "v1:identity:identity:abc",
			"identitySubject":  "user:abc",
			"identityType":     "human",
			"agentId":          "v1:agents:agent:xyz",
			"roleCeiling":      "member",
			"scopes":           []any{"query:*"},
			"createdBySubject": "user:abc",
		}
		if err := s.Validate(bad); err == nil {
			t.Fatalf("roleCeiling=member should be rejected by the concept enum, but validated")
		}

		// agentSubject is no longer a declared field; passing it must be
		// rejected (additionalProperties:false on the tool schema).
		extra := map[string]any{
			"identityId":       "v1:identity:identity:abc",
			"identitySubject":  "user:abc",
			"identityType":     "human",
			"agentId":          "v1:agents:agent:xyz",
			"roleCeiling":      "writer",
			"scopes":           []any{"query:*"},
			"createdBySubject": "user:abc",
			"agentSubject":     "agent:xyz",
		}
		if err := s.Validate(extra); err == nil {
			t.Fatalf("agentSubject should be rejected (not a declared arg), but validated")
		}
	})
}
