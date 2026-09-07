package memql

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/znasllc-io/memql/component/auth"
	"github.com/znasllc-io/memql/component/envregistry"
	langparser "github.com/znasllc-io/memql/component/language/parser"
	"github.com/znasllc-io/memql/component/memql/readiness"
)

// SetReadinessIdentity names this node in the rows it writes.
func (e *MemQLEngine) SetReadinessIdentity(nodeId, nodeType string) {
	if e == nil {
		return
	}
	e.readinessNodeId = strings.TrimSpace(nodeId)
	e.readinessNodeType = strings.TrimSpace(nodeType)
}

// readinessIdentity answers who this node is for the purposes of a readiness
// row. The fallbacks matter: a row keyed on an empty node id would collide
// with every other node's row for the same module, and the fold would then be
// reading one node's opinion while believing it had read the cluster's.
func (e *MemQLEngine) readinessIdentity() (string, string) {
	nodeId, nodeType := e.readinessNodeId, e.readinessNodeType
	if nodeId == "" {
		nodeId = strings.TrimSpace(os.Getenv("MEMQL_NODE_ID"))
	}
	if nodeId == "" {
		if h, err := os.Hostname(); err == nil {
			nodeId = h
		}
	}
	if nodeType == "" {
		nodeType = envregistry.ResolveNodeType()
	}
	return nodeId, nodeType
}

// readinessRowID is the deterministic id a rewrite versions. Deterministic so
// a re-evaluation appends a new VERSION of one logical row rather than a
// second row -- the same reason the cluster singletons sit at literal ids.
func readinessRowID(module, nodeId string) string {
	return ModuleReadinessConcept + ":" + module + "--" + nodeId
}

// renderRecordModuleReadiness renders the @serverOnly call as MemQL TEXT.
// Every string goes through QuoteString -- the lexer's own escaping, which
// diverges from Go's %q on four control characters (memql#4256) -- and the
// lanes ride as a JSON literal, which the parser accepts as a list of objects.
func renderRecordModuleReadiness(r readiness.NodeReport, rowId string) (string, error) {
	lanes := r.Lanes
	if lanes == nil {
		lanes = []readiness.LaneReport{}
	}
	raw, err := json.Marshal(lanes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"mutation recordModuleReadiness(rowId: %s, module: %s, nodeId: %s, nodeType: %s, state: %s, core: %t, lanes: %s, reportedAt: %s)",
		langparser.QuoteString(rowId),
		langparser.QuoteString(r.Module),
		langparser.QuoteString(r.NodeId),
		langparser.QuoteString(r.NodeType),
		langparser.QuoteString(string(r.State)),
		r.Core,
		string(raw),
		langparser.QuoteString(r.ReportedAt.UTC().Format(time.RFC3339)),
	), nil
}

// readinessWriteContext is the engine's own identity for these rows: internal
// origin (what the @serverOnly gate requires) plus a synthetic, unranked
// actor, so the row carries a createdBy and the rank rules do not govern it
// (D4 of the RANK epic). The shape mirrors component/automations'
// contextWithSystemActor.
//
// It REPLACES the caller's actor rather than adding to it, and that is the
// property that makes stamping internal origin here safe on a
// request-derived context (readinessRecompute can be pulled by a cluster
// owner). The stamped context reaches exactly one mutation, every argument of
// which is computed from the manifest and this node's own environment -- no
// caller-supplied value reaches a readiness row, and the caller's authority is
// not carried past this line. Asserted by
// TestReadinessWriteCarriesNoCallerAuthority.
func readinessWriteContext(ctx context.Context) context.Context {
	const actorId = "system:readiness"
	claims := map[string]any{"sub": actorId, "email": actorId, "role": "system"}
	ctx = auth.ContextWithClaims(ctx, claims)
	ctx = auth.ContextWithToken(ctx, auth.BuildTokenInfo(claims))
	ctx = auth.ContextWithAccess(ctx, &auth.AccessContext{
		UserId: actorId,
		// RoleReader, not RoleOwner: this writer needs no cluster-owner
		// escape. The concept is public/requiresIdentity with no owner
		// field, and the write is admitted by @serverOnly plus internal
		// origin, so any more authority than this would be authority
		// nothing here uses.
		Role:      auth.RoleReader,
		Unranked:  true,
		Synthetic: true,
	})
	return auth.ContextWithInternalOrigin(ctx)
}

// WriteModuleReadiness evaluates every module and writes this node's rows as
// new versions of their deterministic ids. Returns how many were written.
//
// A failure stops at the first module and leaves the PREVIOUS version of
// every remaining row standing, which is the right failure: a stale verdict a
// person can act on beats a half-rewritten set nobody can interpret.
func (e *MemQLEngine) WriteModuleReadiness(ctx context.Context) (int, error) {
	manifest, err := envregistry.LoadManifest("")
	if err != nil {
		return 0, fmt.Errorf("module readiness: manifest: %w", err)
	}
	nodeId, nodeType := e.readinessIdentity()
	reports := evaluateModules(ctx, e.readinessResolvers(), manifest.Modules, nodeId, nodeType, time.Now().UTC())
	wctx := readinessWriteContext(ctx)
	written := 0
	for _, r := range reports {
		call, err := renderRecordModuleReadiness(r, readinessRowID(r.Module, nodeId))
		if err != nil {
			return written, fmt.Errorf("module readiness: render %s: %w", r.Module, err)
		}
		if _, err := e.Execute(wctx, call); err != nil {
			return written, fmt.Errorf("module readiness: write %s: %w", r.Module, err)
		}
		written++
	}
	return written, nil
}
