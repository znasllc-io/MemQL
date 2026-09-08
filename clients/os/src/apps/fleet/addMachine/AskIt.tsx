import { useState } from "react";
import { aiChat } from "@znasllc-io/memql-sdk-core/ai";

import { useOsConnection } from "../../../live/connection";
import { Button, Caption, Notice } from "../../../kit";

// ASK IT SOMETHING (design record 2026-09-08-cockpit-install-wizard, D14).
//
// A machine serving a model is proved by using it. One non-streaming chat,
// pinned by the router's explicit-provider seam to `fleet:<modelId>` -- the
// spelling a policy chain uses for a fleet model -- so the call cannot be
// answered by a vendor door and read as the fleet working. The answer, the
// time it took and the door that served it are the whole path the owner asked
// to see: OS to cluster to cockpit to runtime and back, in one act.
//
// THE QUESTION IS FIXED and asks for one word, because the point is the round
// trip and not the prose: a long answer from a small model on a laptop takes
// long enough that the figure stops meaning "is the path alive".
//
// A refusal is the router's own sentence, in surface. The one that matters
// most is `no_local_model_available`: the model is advertised and nothing
// could serve it, which is the fleet door being shut with the light on.

const QUESTION = "Reply with exactly one word: hello.";

export function AskIt({ modelId, machineLabel }: { modelId: string; machineLabel: string }) {
  const connection = useOsConnection();
  const [busy, setBusy] = useState(false);
  const [answer, setAnswer] = useState<{ text: string; ms: number; provider: string } | null>(null);
  const [error, setError] = useState("");

  const provider = `fleet:${modelId}`;

  async function ask(): Promise<void> {
    if (connection === null || busy) return;
    setBusy(true);
    setError("");
    setAnswer(null);
    const started = performance.now();
    try {
      const result = await aiChat(connection.dispatcher, [{ role: "user", content: QUESTION }], { provider });
      const ms = Math.round(performance.now() - started);
      setAnswer({ text: result.message.content.trim(), ms, provider });
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="os-fleet-askit">
      <div className="os-fleet-askit-act">
        <Button tone="primary" busy={busy} busyLabel="Asking..." onClick={() => void ask()} disabled={connection === null}>
          Ask it something
        </Button>
        <Caption>
          Sends "{QUESTION}" through the router, pinned to {modelId} on {machineLabel}.
        </Caption>
      </div>
      {answer === null ? null : (
        <p className="os-fleet-askit-answer" role="status">
          <span className="os-fleet-askit-text">{answer.text === "" ? "(an empty answer)" : answer.text}</span>
          <span className="os-caption">
            {answer.ms} ms, through {answer.provider}
          </span>
        </p>
      )}
      {error === "" ? null : (
        <Notice
          tone="error"
          sentence="The model did not answer."
          next="The router's own reason is below. A model that is advertised and cannot be reached is the fleet door shut with the light on -- the worker log on the machine says why."
          detail={error}
        />
      )}
    </div>
  );
}
