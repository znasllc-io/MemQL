import { installSharedWebLocks } from "./webLocks";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { probeSession } from "../../src/auth/identityClient";
import { identitySource } from "../../src/auth/source";
import { runBufferedDownload } from "../../src/apps/files/actions/download";

beforeEach(installSharedWebLocks);

const CONFIG = {
  identityUrl: "https://identity.example.test",
  identityApiBaseUrl: "https://identity.example.test",
  oauthClientId: "c",
  authEnabled: true,
  domain: "example.test",
};
const credential = (token: string, ttl = 60) =>
  new Response(JSON.stringify({ access_token: token, expires_in: ttl }), { status: 200 });

afterEach(() => vi.useRealTimers());

describe("identitySource HTTP credential freshness", () => {
  it("downloads with a renewed credential after a failed SDK rotation and elapsed lifetime", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const refresh = vi.fn()
      .mockResolvedValueOnce(credential("initial"))
      .mockResolvedValueOnce(new Response("", { status: 503 }))
      .mockResolvedValueOnce(credential("renewed"));
    const source = identitySource(CONFIG, refresh);
    expect(await source.bearer()).toBe("initial");
    vi.advanceTimersByTime(50_000);
    expect(await source.refresh()).toBeNull();
    vi.advanceTimersByTime(11_000);
    const save = vi.fn();
    const fetchContent = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) =>
      (init?.headers as Record<string, string>).Authorization === "Bearer renewed"
        ? new Response("Orchard inventory", { status: 200 })
        : new Response("Unauthorized", { status: 401 }));
    await runBufferedDownload({
      artifactId: "report", fileName: "report.docx", bearer: source.bearer,
      fetchImpl: fetchContent, createObjectUrl: () => "blob:report", revokeObjectUrl: vi.fn(), save,
    });
    expect(save).toHaveBeenCalledWith("blob:report", "report.docx");
    expect(refresh).toHaveBeenCalledTimes(3);
  });

  it("shares one refresh between simultaneous HTTP consumers and SDK rotation", async () => {
    let release!: (response: Response) => void;
    const refresh = vi.fn(() => new Promise<Response>((resolve) => { release = resolve; }));
    const source = identitySource(CONFIG, refresh);
    const pending = [source.bearer(), source.bearer(), source.refresh()];
    await vi.waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
    release(credential("shared"));
    expect(await Promise.all(pending)).toEqual(["shared", "shared", "shared"]);
    expect(await source.bearer()).toBe("shared");
    expect(refresh).toHaveBeenCalledTimes(1);
  });

  it("never falls back to an expired credential when refresh refuses the session", async () => {
    vi.useFakeTimers();
    const refresh = vi.fn().mockResolvedValueOnce(credential("expired"))
      .mockResolvedValueOnce(new Response("", { status: 401 }));
    const source = identitySource(CONFIG, refresh);
    expect(await source.bearer()).toBe("expired");
    vi.advanceTimersByTime(61_000);
    expect(await source.bearer()).toBeNull();
  });
});


describe("identity refresh across independent tabs", () => {
  it("serializes cookie-setting responses across two sources and the sign-in probe", async () => {
    let cookie = "original";
    let current = cookie;
    const presented: string[] = [];
    const fetchRefresh = async () => {
      const sent = cookie;
      presented.push(sent);
      const accepted = sent === current;
      const ordinal = presented.length;
      // Independent servers can all read the same predecessor before writing.
      await Promise.resolve();
      if (!accepted) return new Response("invalid_grant", { status: 401 });
      const issued = `successor-${ordinal}`;
      current = issued;
      // The first response is slow. Without a shared browser lock, later
      // responses overtake it and/or present its already-retired cookie.
      await new Promise((resolve) => setTimeout(resolve, ordinal === 1 ? 30 : 1));
      cookie = issued;
      return credential(issued);
    };
    const firstTab = identitySource(CONFIG, fetchRefresh);
    const secondTab = identitySource(CONFIG, fetchRefresh);
    const results = await Promise.all([firstTab.bearer(), secondTab.bearer(), probeSession(CONFIG, fetchRefresh)]);
    expect(results).toEqual(["successor-1", "successor-2", { signedIn: true }]);
    expect(presented).toEqual(["original", "successor-1", "successor-2"]);
    expect(cookie).toBe(current);
  });

  it("refuses refresh when the browser cannot coordinate tabs", async () => {
    Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
    const fetchRefresh = vi.fn();
    await expect(identitySource(CONFIG, fetchRefresh).bearer()).rejects.toThrow(/Web Locks/);
    await expect(probeSession(CONFIG, fetchRefresh)).rejects.toThrow(/Web Locks/);
    expect(fetchRefresh).not.toHaveBeenCalled();
  });
});
