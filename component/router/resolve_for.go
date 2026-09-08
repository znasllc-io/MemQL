package router

// resolve_for.go -- the router's implementation of the engine's AIResolver
// seam (epic memql#5127, design D2).
//
// The engine cannot import this module (component/router imports
// component/memql and not the other way round), so the seam is an interface
// declared THERE and satisfied here, installed from app/ where both are
// visible. This file is that satisfaction and nothing else: it maps a modality
// onto the Resolve* entry point that serves it, and hands back the client with
// the decision that chose it.
//
// SPEECH AND TRANSCRIPTION ARE ABSENT ON PURPOSE, not by oversight. TTSProvider
// and openAITTSProvider.Synthesize have zero callers anywhere in the tree and
// there is no AiSpeechMsg handler at all -- speech is a wiring gap, not a call
// site. Transcription goes through integrations/stt.StreamingProvider, which
// never touches the provider registry, so there is nothing here for it to
// resolve. Adding either arm now would be a door with no room behind it.

import (
	"context"
	"fmt"

	"github.com/znasllc-io/memql/component/memql"
	"github.com/znasllc-io/memql/core/airoute"
)

// ResolveFor satisfies memql.AIResolver.
func (r *Router) ResolveFor(ctx context.Context, req ResolveRequest) (memql.ResolvedProvider, error) {
	if r == nil {
		return memql.ResolvedProvider{}, memql.ErrAIResolverUnwired
	}
	_ = ctx // the Resolve* entry points carry their own context internally

	var client any
	var resolved Resolved
	var err error

	switch req.Modality {
	case airoute.ModalityStreamingTools:
		client, resolved, err = r.ResolveStreamWithTools(req)
	case airoute.ModalityTools:
		client, resolved, err = r.ResolveWithTools(req)
	case airoute.ModalityChat, airoute.ModalityStreamingChat:
		client, resolved, err = r.ResolveChat(req)
	case airoute.ModalityStructured:
		client, resolved, err = r.ResolveStructured(req)
	case airoute.ModalityVision:
		client, resolved, err = r.ResolveVision(req)
	case airoute.ModalityEmbedding:
		client, resolved, err = r.ResolveEmbedding(req)
	default:
		// A modality the seam does not serve is a CALL-SITE fault, and the
		// message says which: reporting it as an unavailable provider would
		// send somebody to look at their fleet for a call that never named a
		// door.
		return memql.ResolvedProvider{}, fmt.Errorf(
			"the router does not serve modality %q: chat, streamingChat, tools, streamingTools, "+
				"structured, vision and embedding are the seven it resolves", req.Modality)
	}
	if err != nil {
		return memql.ResolvedProvider{}, err
	}
	return memql.ResolvedProvider{
		Client: client,
		Entry:  resolved.Entry,
		Resolution: airoute.Resolution{
			ProviderName: resolved.ProviderName,
			Vendor:       resolved.Vendor,
			Model:        resolved.Model,
			Decision:     resolved.Decision,
		},
	}, nil
}

// Compile-time proof that the seam is satisfied. Without it, a signature
// change on either side is discovered in app/ -- at the one call that installs
// the resolver, in a build-tagged file that not every lane compiles.
var _ memql.AIResolver = (*Router)(nil)
