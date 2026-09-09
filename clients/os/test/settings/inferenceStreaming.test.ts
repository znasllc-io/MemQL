import { expect, it } from "vitest";
import { inferenceFrom, UNREAD_INFERENCE } from "../../src/apps/settings/routingFacts";

it("preserves an explicit plain-stream readiness answer independently of aggregate eligibility", () => {
  expect(inferenceFrom({ payload: { eligible: true, streamingChatEligible: false } }, "").streamingChatEligible).toBe(false);
  expect(inferenceFrom({ payload: { eligible: false, streamingChatEligible: true } }, "").streamingChatEligible).toBe(true);
});

it("keeps absent or malformed plain-stream readiness unknown", () => {
  expect(UNREAD_INFERENCE.streamingChatEligible).toBeUndefined();
  expect(inferenceFrom({ payload: { eligible: true } }, "").streamingChatEligible).toBeUndefined();
  expect(inferenceFrom({ payload: { streamingChatEligible: "true" } }, "").streamingChatEligible).toBeUndefined();
  expect(inferenceFrom(null, "read failed").streamingChatEligible).toBeUndefined();
});
