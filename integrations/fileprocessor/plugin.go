//go:build !planner

package fileprocessor

import (
	fp "github.com/znasllc-io/memql/component/fileprocessor"
	"github.com/znasllc-io/memql/component/memql"
)

// init self-registers the file-processing integration as a plug-in. Not
// compiled into cognition or planner binaries (which don't need text
// extraction -- they work off already-ingested content).
//
// The default processor RESOLVES a vision provider through the router for
// image description (epic memql#5127, design D2); when no door to one is open
// the processor still handles text-based formats (PDF, DOCX, plain text) and
// only image extraction degrades.
func init() {
	memql.RegisterPlugin("files", func(pctx memql.PluginContext) (memql.IntegrationProvider, error) {
		processor := fp.NewDefaultProcessor(pctx.ResolveVisionProvider())
		return NewFilesIntegration(processor), nil
	})
}
