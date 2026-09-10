package planner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"

	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/component/memql"
)

// sealWorkDraft copies reused catalog source before the final validation and
// version stamp. Its original bundle supplies sibling functions and shapes:
// same-domain references need no import, so loading the selected row alone
// would silently leave some of its implementation on the planner replica.
// Shipped concepts and builtins stay imports; no shared registry is changed.
func (l *PlannerAgentLoop) sealWorkDraft(ctx context.Context, owner string, bundle authoringBundle) (authoringBundle, error) {
	out := bundle
	out.Constructs = nil
	seen := map[string]string{}
	add := func(c memql.SandboxConstruct) error {
		key := c.Kind + "/" + c.Name
		if prior, ok := seen[key]; ok {
			if prior != c.Source {
				return fmt.Errorf("work compile: conflicting dependency source for %s", key)
			}
			return nil
		}
		if len(seen) >= 256 {
			return fmt.Errorf("work compile: dependency closure exceeds 256 constructs")
		}
		seen[key] = c.Source
		out.Constructs = append(out.Constructs, c)
		return nil
	}
	for _, c := range bundle.Constructs {
		if err := add(c); err != nil {
			return out, err
		}
	}
	if len(bundle.ReuseEdges) == 0 {
		return out, nil
	}
	if l.engine == nil || strings.TrimSpace(owner) == "" {
		return out, fmt.Errorf("work compile: dependency capture needs its owner's engine")
	}
	ctx = ownerActorContext(ctx, owner)
	read := func(query string) ([]map[string]any, error) {
		res, err := l.engine.Execute(ctx, query)
		if err != nil {
			return nil, err
		}
		rows := memql.MaterializeRows(res)
		for n, row := range rows {
			if payload, ok := row["payload"].(map[string]any); ok {
				flat := maps.Clone(payload)
				flat["id"], flat["concept"] = row["id"], row["concept"]
				rows[n] = flat
			}
		}
		return rows, nil
	}
	owned := func(row map[string]any) bool {
		return memql.BareShortId(getString(row, "ownerUserId")) == memql.BareShortId(owner)
	}
	queue := append([]reuseEdge(nil), bundle.ReuseEdges...)
	refs := map[reuseEdge]bool{}
	bundles := map[string]bool{}
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		ref := queue[0]
		queue = queue[1:]
		if refs[ref] {
			continue
		}
		refs[ref] = true
		if len(refs) > 256 || !workCatalogDependencyKind(ref.Kind) || strings.TrimSpace(ref.Name) == "" {
			return out, fmt.Errorf("work compile: invalid or excessive catalog dependency %s/%s", ref.Kind, ref.Name)
		}
		// The general catalog query paginates at 50. Resolve this exact name
		// directly so an older dependency does not become a false catalog miss.
		query := `concept=="v1:authoring:construct" && catalogued==true && status=="active" && kind==` + langparser.QuoteString(ref.Kind) + ` && name==` + langparser.QuoteString(ref.Name)
		query += ` && ownerUserId==` + langparser.QuoteString("v1:identity:user:"+memql.BareShortId(owner))
		if ref.Namespace != "" {
			query += ` && targetNamespace==` + langparser.QuoteString(ref.Namespace)
		}
		rows, err := read(query)
		if err != nil {
			return out, fmt.Errorf("work compile: resolve dependency %s: %w", ref.Name, err)
		}
		var candidates []map[string]any
		for _, row := range rows {
			if concept := getString(row, "concept"); concept != "" && concept != "v1:authoring:construct" {
				continue // Graph expansion may also return the parent bundle.
			}
			if !owned(row) || row["kind"] != ref.Kind || row["name"] != ref.Name || row["status"] != "active" || row["catalogued"] != true || (ref.Namespace != "" && row["targetNamespace"] != ref.Namespace) {
				return out, fmt.Errorf("work compile: dependency %s did not resolve to its owner's active catalog", ref.Name)
			}
			candidates = append(candidates, row)
		}
		if len(candidates) != 1 {
			return out, fmt.Errorf("work compile: dependency %s/%s has %d catalog matches, need one", ref.Kind, ref.Name, len(candidates))
		}
		selected := candidates[0]
		source := getString(selected, "source")
		if strings.TrimSpace(source) == "" {
			return out, fmt.Errorf("work compile: dependency %s has no source", ref.Name)
		}
		if err := add(memql.SandboxConstruct{Kind: ref.Kind, Name: ref.Name, Source: source}); err != nil {
			return out, err
		}
		bundleID := getString(selected, "bundleId")
		if bundleID == "" {
			return out, fmt.Errorf("work compile: dependency %s has no source bundle", ref.Name)
		}
		if bundles[bundleID] {
			continue
		}
		bundles[bundleID] = true
		rows, err = read("query authoringBundleById(" + encodeArgs(map[string]any{"bundleId": bundleID}) + ")")
		if err != nil || len(rows) != 1 || !owned(rows[0]) {
			return out, fmt.Errorf("work compile: dependency %s source bundle is not readable: %v", ref.Name, err)
		}
		if value := rows[0]["reusedConstructRefs"]; value != nil {
			encoded, err := json.Marshal(value)
			if err != nil {
				return out, err
			}
			var nested []reuseEdge
			if err := json.Unmarshal(encoded, &nested); err != nil {
				return out, fmt.Errorf("work compile: malformed dependency references: %w", err)
			}
			queue = append(queue, nested...)
		}
		rows, err = read("query authoringConstructsForBundle(" + encodeArgs(map[string]any{"bundleId": bundleID}) + ")")
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			kind := getString(row, "kind")
			if !workCatalogDependencyKind(kind) {
				continue
			}
			if !owned(row) || memql.BareShortId(getString(row, "bundleId")) != memql.BareShortId(bundleID) || row["status"] != "active" {
				return out, fmt.Errorf("work compile: dependency bundle contains an unavailable %s", getString(row, "name"))
			}
			c := memql.SandboxConstruct{Kind: kind, Name: getString(row, "name"), Source: getString(row, "source")}
			if c.Name == "" || strings.TrimSpace(c.Source) == "" {
				return out, fmt.Errorf("work compile: dependency bundle contains an incomplete construct")
			}
			if err := add(c); err != nil {
				return out, err
			}
		}
	}
	sort.Slice(out.Constructs, func(i, j int) bool {
		a, b := out.Constructs[i], out.Constructs[j]
		return a.Kind < b.Kind || (a.Kind == b.Kind && a.Name < b.Name)
	})
	return out, nil
}

func workCatalogDependencyKind(kind string) bool {
	switch kind {
	case "query", "mutation", "logic", "spec", "trait", "shape":
		return true
	default:
		return false
	}
}
