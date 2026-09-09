import { afterEach, describe, expect, it, vi } from "vitest";
import { identitySource } from "../../src/auth/source";
import { runBufferedDownload } from "../../src/apps/files/actions/download";

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
    expect(refresh).toHaveBeenCalledTimes(1);
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
