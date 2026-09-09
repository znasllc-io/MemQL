// Ask's real transport (spec D6): sdk-core ai chat streaming over the
// shell's one connection, behind the same interface the PR A stub filled.
// The context tag rides as a labelled system line, so an app-scoped
// question carries its scope without the surface changing shape.

import { aiChatStream, type AiChatMessage } from "@znasllc-io/memql-sdk-core/ai";
import type { Dispatcher } from "@znasllc-io/memql-sdk-core/client";

import type { AskCallbacks, AskHandle, AskTransport } from "./askController";

export type AskStreamFn = (
  dispatcher: Dispatcher,
  messages: AiChatMessage[],
  opts: { signal?: AbortSignal },
) => { deltas: AsyncIterable<{ textDelta?: string }>; result: Promise<unknown> };

export class SdkAskTransport implements AskTransport {
  constructor(
    private readonly dispatcher: () => Dispatcher | null,
    private readonly stream: AskStreamFn = aiChatStream,
  ) {}

  ask(prompt: string, context: string | null, on: AskCallbacks): AskHandle {
    const dispatcher = this.dispatcher();
    if (!dispatcher) {
      // Honest, in-surface, retryable -- never a toast (spec C).
      queueMicrotask(() => on.error("Not connected to the cluster yet."));
      return { cancel: () => {} };
    }

    const abort = new AbortController();
    const messages: AiChatMessage[] = [
      ...(context ? [{ role: "system", content: `Context: ${context}` }] : []),
      { role: "user", content: prompt },
    ];

    void (async () => {
      try {
        const handle = this.stream(dispatcher, messages, { signal: abort.signal });
        let settled = false;
        let answer = "";
        // Observe failure concurrently: a failed result must not wait behind
        // an iterator whose peer has gone away.
        const result = handle.result.then(
          (value) => { settled = true; return value; },
          (error: unknown) => { settled = true; throw error; },
        );
        const consume = async () => {
          for await (const delta of handle.deltas) {
            if (abort.signal.aborted) return;
            if (delta.textDelta) { answer += delta.textDelta; on.delta(delta.textDelta); }
          }
          // sdk-core closes deltas only with a terminal result/error. An
          // earlier end is a broken stream, not a slow cold model.
          if (!abort.signal.aborted && !settled) throw new Error("The reply stream ended before completion. Try again.");
        };
        const [, terminal] = await Promise.all([consume(), result]);
        if (abort.signal.aborted) return;
        if (!answer.trim() && terminal && typeof terminal === "object" && "message" in terminal) {
          const message = terminal.message;
          if (message && typeof message === "object" && "content" in message && typeof message.content === "string") {
            answer = message.content;
            if (answer) on.delta(answer);
          }
        }
        if (!answer.trim()) throw new Error("The cluster finished without an answer. Try again.");
        on.done();
      } catch (err) {
        if (abort.signal.aborted) return;
        abort.abort();
        on.error(err instanceof Error ? err.message : "The cluster did not answer.");
      }
    })();

    return { cancel: () => abort.abort() };
  }
}
