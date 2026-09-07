import { Button, Caption } from "../../kit";
import { devicesUrl, leaveForPasskey } from "./returns";

// The passkey stop's body (design record 2026-09-06-first-run-wizard, D6).
//
// THE OS RUNS NO WEBAUTHN CEREMONY OF ITS OWN. A second implementation of a
// security ceremony is the last thing that should have two, so this is a link
// to the identity origin and a re-read on the way back -- never a registration
// form. `/enroll` is deliberately not offered here: it authorizes a person who
// cannot sign in at all, and everybody reading this widget is signed in.

export function PasskeyStop({
  identityUrl,
  win = globalThis.window,
}: {
  identityUrl: string;
  /** Injected so the leave can be asserted without navigating the test. */
  win?: Window;
}) {
  const url = devicesUrl(identityUrl);

  // NO "you already have one" READING HERE. A finished stop is not a
  // disclosure on this rail, so a body for it would be unreachable -- and
  // managing passkeys is the identity service's surface, which the Cluster
  // app's Readiness section already links.
  return (
    <div className="os-setup-stop">
      {url === "" ? (
        // NO BUTTON WITH NOWHERE TO GO (interface rule 12). The shell has not
        // been told where its identity service lives, so the destination is
        // named in words.
        <Caption>Register one on the identity service, under Devices.</Caption>
      ) : (
        <>
          <div className="os-setup-stop-act">
            <Button tone="primary" onClick={() => leaveForPasskey(identityUrl, win)}>
              Register a passkey
            </Button>
          </div>
          <Caption>This opens the identity service. You will land back here.</Caption>
        </>
      )}
    </div>
  );
}
