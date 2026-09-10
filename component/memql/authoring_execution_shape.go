package memql

import (
	"context"
	"fmt"
	"strings"

	"github.com/znasllc-io/memql/component/auth"
	languageParser "github.com/znasllc-io/memql/component/language/parser"
)

// resolveNamedShapeForContext keeps a work bundle's shapes private to its
// authenticated owner and run. Shared shapes retain precedence under both
// their direct and concept-qualified spellings. Nothing is installed globally.
func (e *MemQLEngine) resolveNamedShapeForContext(ctx context.Context, name, sourceFunction string) (shapeTemplate, error) {
	scope, scoped := ctx.Value(authoredExecutionKey{}).(authoredExecution)
	access, authenticated := auth.AccessFromContext(ctx)
	owned := scoped && scope.registry != nil && authenticated && access != nil && access.UserId != "" && access.UserId == scope.owner
	// Authored functions retain their source imports, but do not pass through
	// bootstrap's reference rewrite. Resolve aliases from the calling query's
	// source, never from another query in the bundle or a shadowed core query.
	if owned && sourceFunction != "" {
		caller := sourceFunction
		var coreCaller bool
		if e.functions != nil {
			fn, err := e.functions.Get(caller)
			coreCaller = err == nil && fn != nil
		}
		if !coreCaller {
			_, callerName := SplitConstructKey(caller)
			if query, found := scope.registry.Resolve(scope.owner, "query", callerName); found {
				uses, err := parsedUseDeclarations(query.Source)
				if err != nil {
					return nil, fmt.Errorf("authored query %q shape imports: %w", caller, err)
				}
				if imported, found := NewConstructScope("", uses).Imports[name]; found {
					name = QualifyConstruct(imported.Namespace, imported.SourceName)
				}
			}
		}
	}
	_, bare := SplitConstructKey(name)
	for _, candidate := range []string{name, bare} {
		if _, found := e.shapes.Get(candidate); found {
			return e.resolveNamedShape(name)
		}
	}
	if !owned {
		return e.resolveNamedShape(name)
	}
	entry, found := scope.registry.Resolve(scope.owner, "shape", name)
	if !found && bare != name {
		entry, found = scope.registry.Resolve(scope.owner, "shape", bare)
	}
	if !found {
		return e.resolveNamedShape(name)
	}

	decl, err := languageParser.ParseShapeDecl(stripUseDeclarations(entry.Source))
	if err != nil {
		return nil, fmt.Errorf("authored shape %q: %w", name, err)
	}
	if decl.Name != entry.Name {
		return nil, fmt.Errorf("authored shape %q source declares %q", entry.Name, decl.Name)
	}
	def, err := shapeDeclToShapeDefinition(decl, "authored:shape:"+entry.Name)
	if err != nil {
		return nil, err
	}
	private := newShapeRegistry()
	if err := private.add(def); err != nil {
		return nil, err
	}
	if violations := validateShapeConceptBindings(private, e.concepts); len(violations) > 0 {
		return nil, fmt.Errorf("authored shape %q: %s", name, violations[0].Detail)
	}
	// Caller-authored explicit projections must obey the same internal-field
	// boundary as default projections. An unbound payload path cannot prove
	// that boundary; intrinsic-only and actor-only shapes need no concept.
	var internalFields []string
	if len(def.UseConcepts) > 0 {
		bound, err := resolveShapeBoundConcept(e.concepts, def)
		if err != nil {
			return nil, fmt.Errorf("authored shape %q: %w", name, err)
		}
		internalFields = bound.InternalFields()
	}
	for _, key := range sortedTemplateKeys(def.Template) {
		stored, ok := def.Template[key].(string)
		if !ok {
			continue
		}
		path, payload := strings.CutPrefix(extractNodePath(stored), "payload.")
		if !payload {
			continue
		}
		if len(def.UseConcepts) == 0 {
			return nil, fmt.Errorf("authored shape %q payload projection requires a bound concept", name)
		}
		for _, internal := range internalFields {
			if path == internal || strings.HasPrefix(path, internal+".") {
				return nil, fmt.Errorf("authored shape %q projects internal field %q", name, internal)
			}
		}
	}
	if def.DefaultProjection && expandDefaultShapeProjections(nil, private, e.concepts) != 1 {
		return nil, fmt.Errorf("authored shape %q default projection could not be resolved", name)
	}
	return convertShapeDefinitionTemplate(def.Template)
}
