import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { SignIn } from "../../src/chrome/SignIn";
import { installSharedWebLocks } from "./webLocks";

afterEach(cleanup);

describe("sign-in browser requirements", () => {
  it("explains an unsupported browser instead of offering unsafe concurrent refresh", () => {
    Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
    render(<SignIn status="unavailable" onSignIn={() => {}} />);
    expect(screen.getByText(/Update your browser/)).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Sign in" })).toBeNull();
  });

  it("offers normal sign-in when the browser can coordinate tabs", () => {
    installSharedWebLocks();
    render(<SignIn status="signed-out" onSignIn={() => {}} />);
    expect(screen.getByRole("button", { name: "Sign in" })).toBeTruthy();
  });
});
