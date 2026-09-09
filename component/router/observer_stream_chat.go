package router

import (
	"context"
	"time"

	"github.com/znasllc-io/memql/core/common"
)

// observedStreamChat wraps a ChatStreamProvider. It starts
// a timer on CallChatStream, watches the delta channel for the
// first text chunk (time-to-first-token), accumulates output character
// count, and on stream close emits one CallRecord via the router.
type observedStreamChat struct {
	inner    common.ChatStreamProvider
	router   *Router
	resolved Resolved
	req      ResolveRequest
}

// CallChatStream implements common.ChatStreamProvider.
// Token counts are Phase-1 estimates; see estimate.go.
func (o *observedStreamChat) CallChatStream(
	ctx context.Context,
	messages []common.ChatMessage,
) (<-chan common.StreamChunk, error) {
	start := time.Now()
	inputTokens := EstimateMessageTokens(messages)

	innerCh, err := o.inner.CallChatStream(ctx, messages)
	if err != nil {
		o.router.recordCall(buildRecord(o.req, o.resolved, o.inner, inputTokens, 0, 0, start, time.Time{}, time.Now(), true, err, ctx.Err()))
		return nil, err
	}

	observedCh := make(chan common.StreamChunk)
	go func() {
		defer close(observedCh)

		var (
			firstTokenAt time.Time
			outputChars  int
			chunkErr     error
		)

		defer func() {
			end := time.Now()
			outputTokens := EstimateTokensFromChars(outputChars)
			rec := buildRecord(o.req, o.resolved, o.inner, inputTokens, outputTokens, 0, start, firstTokenAt, end, true, chunkErr, ctx.Err())
			// Tokens per second is only meaningful for streaming calls
			// with a non-trivial duration. Guard both so voice-path
			// one-second replies don't produce gigatokens/sec noise.
			durSec := end.Sub(start).Seconds()
			if outputTokens > 0 && durSec > 0.1 {
				rec.TokensPerSec = float64(outputTokens) / durSec
			}
			o.router.recordCall(rec)
		}()

		for {
			var chunk common.StreamChunk
			var ok bool
			select {
			case <-ctx.Done():
				return
			case chunk, ok = <-innerCh:
				if !ok {
					return
				}
			}
			if chunk.Error != nil && chunkErr == nil {
				chunkErr = chunk.Error
			}
			if firstTokenAt.IsZero() && chunk.Content != "" {
				firstTokenAt = time.Now()
			}
			outputChars += len(chunk.Content)
			select {
			case observedCh <- chunk:
			case <-ctx.Done():
				// The deferred observer records cancellation before closing.
				return
			}
		}

	}()
	return observedCh, nil
}
