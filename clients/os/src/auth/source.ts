// The OS's credential seam (spec D7, the portal's identityAuthSource
// pattern at OS size). The access token lives in a closure -- components
// ask for capability ("bearer()"), never for the string, so there is
// exactly one place it can leak from. Refresh goes through the HttpOnly
// cookie; the SDK owns the rotation timer. HTTP consumers also check the
// returned lifetime on demand, so a failed SDK rotation cannot strand them
// with an expired credential after the cluster recovers.

import type { OsRuntimeConfig } from "../cluster/config";
import { refreshAccessCredential, type IdentityFetch } from "./identityClient";

export interface OsAuthSource {
  /** The credential to dial or fetch with right now, or null for none. */
  bearer(): Promise<string | null>;
  /** A FRESH credential (the SDK's rotation hook). Null = give up. */
  refresh(): Promise<string | null>;
}

/** Auth disabled (or signed out): supply nothing, honestly. */
export const anonymousSource: OsAuthSource = {
  bearer: async () => null,
  refresh: async () => null,
};

export function identitySource(
  config: OsRuntimeConfig,
  fetchImpl: IdentityFetch = fetch,
): OsAuthSource {
  let held: { bearer: string; refreshAt: number } | null = null;
  let renewing: Promise<string | null> | null = null;
  const renew = (): Promise<string | null> => {
    // One cookie rotation serves concurrent downloads, uploads and SDK calls.
    if (renewing) return renewing;
    if (!config.authEnabled) return Promise.resolve(null);
    const startedAt = Date.now();
    renewing = (async () => {
      try {
        const fresh = await refreshAccessCredential(config, fetchImpl);
        if (fresh === null) return null;
        // Renew at 90% of the issued lifetime, measured entirely on this clock.
        held = { bearer: fresh.bearer, refreshAt: startedAt + fresh.expiresInSeconds * 1000 * 0.9 };
        return held.bearer;
      } finally {
        renewing = null;
      }
    })();
    return renewing;
  };
  return {
    bearer: async () => held !== null && Date.now() < held.refreshAt ? held.bearer : renew(),
    refresh: renew,
  };
}
