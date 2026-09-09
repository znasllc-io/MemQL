import { Caption, Check, ChoiceStack, Field, Input, Notice, type ChoiceOption } from "../../../../kit";
import type { Draft } from "../flow";
import { INSTALL_PLATFORMS, INSTALL_PLATFORM_LABEL, type InstallPlatform } from "../install";

// THIS MACHINE: what it is called, and what it will run. The form, and the
// only stop the person answers by typing.
//
// The two choices are INPUTS to the mint -- they decide which install
// command the next stop shows -- so they stay with the name that produces
// it. They are checkboxes in a form, which is what rule 10 of the interface
// language reserves them for: a choice being stated, not in-surface state.

const PLATFORMS: readonly (ChoiceOption & { value: InstallPlatform })[] = INSTALL_PLATFORMS.map(
  (platform) => ({ value: platform, label: INSTALL_PLATFORM_LABEL[platform] }),
);

export function MachineStop({
  draft,
  onDraft,
  connected,
  mintError,
  onMint,
}: {
  draft: Draft;
  onDraft: (patch: Partial<Draft>) => void;
  connected: boolean;
  /** The server's refusal, verbatim, or "". */
  mintError: string;
  /** Enter in the name field. The bar's act decides whether a mint is legal;
   *  this only asks. */
  onMint: () => void;
}) {
  return (
    <div className="os-stop-body os-fleet-addstop">
      {mintError === "" ? null : (
        <Notice
          tone="error"
          sentence="The token was not minted."
          next="Nothing was created; mint again."
          detail={mintError}
        />
      )}

      <Field label="What is this machine called">
        <Input
          id="fleet-add-name"
          label="What is this machine called"
          value={draft.name}
          placeholder={draft.platform === "linux" ? "pop-os-desktop" : "studio-mac-mini"}
          onChange={(name) => onDraft({ name })}
          onEnter={onMint}
        />
      </Field>
      <Caption>
        Yours, for the credential and for the list. The machine reports its own hostname when
        it connects; this name is put on it the moment it does.
      </Caption>

      <ChoiceStack
        name="fleet-add-platform"
        label="Operating system"
        voice="prose"
        value={draft.platform}
        onChange={(next) => onDraft({ platform: next as InstallPlatform })}
        options={PLATFORMS}
      />

      <Check checked={draft.userLocal ?? false} onChange={(userLocal) => onDraft({ userLocal })}>
        Install for my account only, without a password, in ~/.memql/bin. Leave this off to
        install a protected system command in /usr/local/bin using your account password.
      </Check>

      <Check checked={draft.computerUse} onChange={(computerUse) => onDraft({ computerUse })}>
        Install the computer-use build (mouse, keyboard, screenshots).{" "}
        {draft.platform === "mac"
          ? "macOS asks for Accessibility and Screen Recording the first time it runs."
          : "Mouse and keyboard control require an X11 desktop session. Local models and other tools also work on Wayland."}
      </Check>

      <Check checked={draft.inference} onChange={(inference) => onDraft({ inference })}>
        This machine will run local models. After installation, run a second command to check
        the hardware, approve the runtime setup and download the recommended models. Allow
        several gigabytes of disk space and time for the download.
      </Check>

      {connected ? null : (
        <Caption>A token can only be minted over a live connection to the cluster.</Caption>
      )}
    </div>
  );
}
