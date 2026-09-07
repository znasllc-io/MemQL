import { useEffect, useState } from "react";

import { Button, Caption, Field, Head, Input, Notice, Panel, Subhead } from "../../kit";
import { findRegion, revealRegion } from "../../kit";
import { useSession } from "../../chrome/access";
import type { OsAppProps } from "../../system/registry";
import type { RoleRequirement } from "../../system/roles";
import {
  doorFor,
  federationFields,
  localityOf,
  missingFederationFields,
  sourceCopy,
  summarize,
  useFleetDoor,
  useProviderActions,
  useProviderRegistry,
  vendorLabel,
  type DoorState,
  type FleetDoor,
  type IssuerReach,
  type VendorDoor,
} from "./providerFacts";

// AI providers (epic memql#4984; rebuilt by epic memql#5088): which doors to a
// model this cluster has, and what to do about the ones that are shut.
//
// THE SCREEN IS A POSTURE, NOT A CREDENTIALS FORM. There is no API key here,
// or anywhere else in the product: a typed key is a long-lived credential at
// rest, and the owner's decision was to remove every one of them, the local
// cluster included. What is left is two federated doors and the fleet, and the
// section has to read as a deliberate security position rather than as a
// feature somebody deleted.
//
// FIVE RULES, and they are the design of this surface:
//
//  1. THE STATE IS A COLUMN, AND IT IS THE CONTENT. Each door opens on one
//     line -- [state word] [what that means] -- and the three panels share the
//     column, so the words stack and a person reads the whole cluster by
//     scanning one column. Not a status pill in a card corner: a pill puts the
//     answer in the furniture, and its palette has no way to say "nothing here,
//     and that is correct".
//  2. AN UNSET DOOR IS THE QUIETEST THING ON THE SCREEN. Installing spends no
//     inference and asks for nothing, so a fresh cluster has no federated
//     vendor -- and every local cluster has none permanently. That state gets a
//     hairline and a plain sentence. An operator who meets a warning banner on
//     a fresh install concludes the install failed.
//  3. HALF-CONFIGURED IS THE ONE THAT SHOUTS, because the engine REFUSES BOOT
//     on it. One to three of Anthropic's ids, or one of OpenAI's two, and the
//     fleet goes down at its next restart -- hours after the save that caused
//     it. It is the only door state that carries the warn rule, and it renders
//     the engine's own sentence, which names which ids are set and which are
//     missing better than a re-derivation here would.
//  4. A LOCAL CLUSTER IS TOLD, NOT ASKED (design D6). Its OIDC issuer is
//     private, so neither vendor can discover it and no id anybody types will
//     ever work. The form is ABSENT there with a sentence in its place, which
//     is DESIGN.md rule 12's position: an act that is not legal is absent,
//     never disabled. The fleet panel moves to the top, because there it is the
//     only door.
//  5. VERIFY REACHES A VENDOR, SO A PERSON PRESSES IT. A live credential check
//     spends somebody's quota with a third party. It is never something a panel
//     does on render, and a refusal from the vendor is a RESULT rendered in the
//     vendor's own words -- not an exception blamed on this console.
//
// AND A SAVE IS STILL NOT AN APPLY. Saving writes the ids; the registry each
// node resolved at boot does not move until Apply broadcasts. Two controls,
// because they are two facts.

/**
 * The section's role requirement (epic memql#5088, D7).
 *
 * OWNER OR DEVELOPER, AS A SET, EXPLICITLY NOT ADMIN -- the same shape and the
 * same argument as Settings -> Integrations. A developer helps an owner through
 * setup, so the four provider builtins move to the owner-or-developer set; an
 * admin's concern is USER ADMINISTRATION, and the ladder puts admin (200) below
 * developer (300), so `{ min: "developer" }` would admit exactly the role the
 * engine refuses and offer them a form that fails field by field.
 *
 * Presentation only; every gate is server-side. The manifest declares it and
 * this constant is the one copy of the value.
 */
export const PROVIDERS_SECTION_ROLE: RoleRequirement = { any: ["owner", "developer"] };

/**
 * The state words, in one place because they are the section's whole
 * vocabulary and the suite reads them from here rather than restating them.
 *
 * `closed` and `unset` are the SAME door state wearing two words, and the
 * distinction is worth the second word: "Not set up" invites somebody to set
 * it up, which on a local cluster is an invitation to waste an afternoon.
 */
export const DOOR_WORDS = {
  open: "Open",
  unset: "Not set up",
  closed: "Closed",
  half: "Half set up",
  unknown: "Not read",
} as const;

export function ProvidersSection({
  intent,
  consumeIntent,
}: {
  intent?: OsAppProps["intent"];
  consumeIntent?: OsAppProps["consumeIntent"];
} = {}) {
  const { access, config } = useSession();
  const registry = useProviderRegistry(true);
  const fleet = useFleetDoor(true);
  const actions = useProviderActions(registry.reload);
  const reach = localityOf(config.domain);

  const anthropic = (
    <div key="anthropic" data-os-vendor="anthropic">
      <VendorPanel
      vendor="anthropic"
      reach={reach}
      door={doorFor("anthropic", registry.rows)}
      busy={actions.state.busy}
      onSave={(fields) => void actions.saveFederation("anthropic", fields)}
      />
    </div>
  );
  const openai = (
    <div key="openai" data-os-vendor="openai">
      <VendorPanel
      vendor="openai"
      reach={reach}
      door={doorFor("openai", registry.rows)}
      busy={actions.state.busy}
      onSave={(fields) => void actions.saveFederation("openai", fields)}
      />
    </div>
  );
  const machines = <FleetPanel key="fleet" door={fleet} />;

  // THE ORDER IS THE RECOMMENDATION. Federation leads on a cluster that can
  // federate; on one that cannot, leading with two doors nobody can open would
  // bury the only one that works under the explanation of why the others do
  // not.
  const doors = reach === "local" ? [machines, anthropic, openai] : [anthropic, openai, machines];

  // ARRIVING AT A VENDOR (epic memql#5106). The first-run wizard's Anthropic
  // and OpenAI doors send somebody here to set that vendor up, and this page
  // carries three panels -- so landing at the top of it leaves the last step
  // of the act to them. The intent names the vendor; this brings its panel
  // into view and puts the cursor in its first field.
  //
  // CONSUMED BY ID, so a stale render cannot scroll the page out from under
  // somebody who has since scrolled somewhere else.
  //
  // ON A LOCAL CLUSTER THE PANEL IT REVEALS HAS NO FORM (epic memql#5088): a
  // private OIDC issuer cannot be federated with, so the panel carries the
  // reason instead of fields. revealRegion optional-chains both the query and
  // the focus, so that is a scroll to an explanation rather than a crash --
  // and delivering somebody to the surface that says why there is nothing to
  // fill in is the right answer, not a degraded one.
  const vendor = typeof intent?.payload["vendor"] === "string" ? intent.payload["vendor"] : "";
  useEffect(() => {
    if (!intent || vendor === "") return;
    revealRegion(findRegion("os-vendor", vendor));
    consumeIntent?.(intent.id);
  }, [intent, vendor, consumeIntent]);

  return (
    <div className="os-settings">
      <Head
        title="AI providers"
        meta={registry.rows.length === 0 ? undefined : `${registry.rows.length} registered`}
      >
        <Button
          tone="primary"
          onClick={() => void actions.apply()}
          busy={actions.state.busy}
          busyLabel="Applying"
        >
          Apply
        </Button>
      </Head>
      <p className="os-caption">
        Which doors to a model this cluster has, and how it proves it may use
        them. Federation is the only door for a cloud vendor -- there is no API
        key to enter, here or anywhere else in the product -- and a machine you
        own is the other one.
      </p>

      {registry.error ? (
        <Notice
          tone="warn"
          sentence={`The cluster declined this read for ${access?.clusterRole || "your role"}.`}
          detail={registry.error}
        />
      ) : null}

      {actions.state.message ? (
        <Notice
          tone={actions.state.failed ? "error" : "info"}
          sentence={actions.state.failed ? "That did not go through." : "Done."}
          detail={actions.state.message}
        />
      ) : null}

      {doors}

      <Panel label="What this node can call">
        <Subhead>What this node can call</Subhead>
        <Caption>
          {registry.loading && registry.rows.length === 0
            ? "Reading the registry."
            : summarize(registry.rows).headline}
        </Caption>
        {registry.rows.length === 0 ? null : (
          <ul className="os-hidden-list" aria-label="Registered providers">
            {registry.rows.map((p) => (
              <li key={p.name}>
                <span
                  className="os-dot"
                  data-os-dot={p.available ? "reachable" : "unreachable"}
                  role="img"
                  aria-label={p.available ? "can be called" : "cannot be called"}
                />{" "}
                <span className="os-mono">{p.name}</span> -- {vendorLabel(p.vendor)} {p.model},
                credential from {sourceCopy(p.authSource)}
                {p.reason ? ` -- ${p.reason}` : ""}{" "}
                <Button
                  onClick={() => void actions.verify(p.name)}
                  busy={actions.state.busy}
                  busyLabel="Asking"
                  ariaLabel={`Verify ${p.name} with the vendor`}
                >
                  Verify
                </Button>
              </li>
            ))}
          </ul>
        )}
        <div className="os-refresh-row">
          <Button onClick={registry.reload} busy={registry.loading} busyLabel="Reading">
            Refresh
          </Button>
          <Caption>
            {registry.fetchedAt === null
              ? ""
              : `Read at ${new Date(registry.fetchedAt).toISOString()}. `}
            One node&apos;s own registry -- which replica answered is not
            knowable from here, which is why Apply broadcasts rather than
            relying on repeated reads.
          </Caption>
        </div>
      </Panel>
    </div>
  );
}

/**
 * One door: the state, what it means, and the detail under it.
 *
 * TWO COLUMNS, SHARED ACROSS THE PANELS, so the state words line up and can be
 * read as a column. The rule down the left carries the state a second time in
 * colour and weight, and an open door is the only one with a filled ground --
 * three channels, of which the word is the one that survives greyscale, a
 * colour-blind reader and a screenshot.
 */
function Door({
  state,
  word,
  children,
}: {
  state: DoorState | "unknown";
  word: string;
  children: React.ReactNode;
}) {
  return (
    <div className="os-door" data-os-door={state}>
      <p className="os-door-state">{word}</p>
      <p className="os-door-said">{children}</p>
    </div>
  );
}

function VendorPanel({
  vendor,
  reach,
  door,
  busy,
  onSave,
}: {
  vendor: string;
  reach: IssuerReach;
  door: VendorDoor;
  busy: boolean;
  onSave: (fields: Record<string, string>) => void;
}) {
  const label = vendorLabel(vendor);
  // A local cluster's door is shut and stays shut, so it takes the word that
  // does not invite anybody to open it.
  const word =
    door.state === "open"
      ? DOOR_WORDS.open
      : door.state === "half"
        ? DOOR_WORDS.half
        : reach === "local"
          ? DOOR_WORDS.closed
          : DOOR_WORDS.unset;

  return (
    <Panel label={label}>
      <div className="os-door-panel">
        <Subhead>{label}</Subhead>
        <Door state={door.state} word={word}>
          {door.state === "open" ? (
            <>
              Every pod exchanges its own projected Kubernetes token for a
              bearer that lasts an hour. Nothing about this credential is stored
              -- not here, not in a secret, not on disk.
            </>
          ) : door.state === "half" ? (
            <>
              Some of {label}&apos;s ids are set and some are not, and a node
              that reads a partial set refuses to boot. Fix it before anything
              restarts -- and re-enter every id below, not only the missing
              one: the write stores the set whole.
            </>
          ) : reach === "local" ? (
            <>
              A local cluster&apos;s OIDC issuer is private, so {label} cannot
              discover it and no id typed here would ever be accepted. {label}{" "}
              models cannot be called from this cluster.
            </>
          ) : (
            <>
              Nothing is set here yet, which is the normal state of a new
              cluster. {label} models cannot be called until it is, and nothing
              else on this cluster depends on it.
            </>
          )}
        </Door>

        {/* The engine's own sentence, whole. It names which ids are set and
            which are missing, and any paraphrase here would drop one half. */}
        {door.said === "" ? null : (
          <p className="os-door-detail os-mono">{door.said}</p>
        )}

        {reach === "local" ? (
          <Caption>
            Uploading a JWKS per developer cluster is not reproducible, so this
            is not a gap waiting to be closed. Use a machine you own instead --
            it is the door a local cluster has.
          </Caption>
        ) : (
          <FederationForm
            vendor={vendor}
            inUse={door.state === "open"}
            busy={busy}
            onSave={onSave}
          />
        )}
      </div>
    </Panel>
  );
}

/**
 * The ids, and the one control that applies them.
 *
 * ALL-OR-NONE, CHECKED HERE AS WELL AS SERVER-SIDE, and the reason is worth
 * knowing: a partial set REFUSES BOOT. The engine enforces it before the write
 * so a half-set cannot be stored; refusing here too means the person is told
 * WHICH id is missing while they are still looking at it, rather than after a
 * round trip.
 *
 * NONE OF THESE IS A CREDENTIAL. They are opaque vendor identifiers stored as
 * plaintext rows, which is why the field is an ordinary text box, is never
 * masked, and shows what was typed.
 */
function FederationForm({
  vendor,
  inUse,
  busy,
  onSave,
}: {
  vendor: string;
  /** Whether ids are already applied, which changes what saving MEANS. */
  inUse: boolean;
  busy: boolean;
  onSave: (fields: Record<string, string>) => void;
}) {
  const [draft, setDraft] = useState<Record<string, string>>({});
  const fields = federationFields(vendor);
  const missing = missingFederationFields(vendor, draft);
  const label = vendorLabel(vendor);
  // UNTOUCHED IS NOT INCOMPLETE. An empty form has not failed anything, and
  // naming its blank fields the moment the panel opens turns the normal state
  // of every new cluster into a list of complaints -- and on a half-configured
  // panel it contradicts the engine's own sentence above it, which has just
  // named some of those ids as set. The list appears once somebody is filling
  // it in, which is when it helps.
  const touched = Object.values(draft).some((v) => v !== "");

  const submit = () => {
    // Trimmed, and a blank optional field is OMITTED rather than written
    // empty: an empty row is not the same as no row, and the engine's
    // all-or-none check counts what arrives.
    const out: Record<string, string> = {};
    for (const f of fields) {
      const value = (draft[f.key] ?? "").trim();
      if (value !== "") out[f.key] = value;
    }
    onSave(out);
  };

  return (
    <>
      {fields.map((f) => (
        <Field key={f.key} label={f.required ? f.label : `${f.label} (optional)`}>
          <Input
            id={`provider-fed-${vendor}-${f.key}`}
            label={f.label}
            value={draft[f.key] ?? ""}
            onChange={(next) => setDraft((held) => ({ ...held, [f.key]: next }))}
            placeholder={f.hint || undefined}
          />
        </Field>
      ))}
      <div className="os-refresh-row">
        <Button
          tone="primary"
          onClick={submit}
          disabled={missing.length > 0}
          busy={busy}
          busyLabel="Saving"
        >
          Save {label} federation
        </Button>
        <Caption>
          {!touched
            ? inUse
              ? // An empty form under an OPEN door reads as unfinished unless
                // it says what filling it in would do. Rotating onto a
                // different service account is a real act and has to be
                // reachable; it is just not the act this panel is about.
                `${label}'s ids are already in use. Filling these in replaces them at the next Apply -- nothing changes until then.`
              : `Ids from ${label}'s own console, not credentials -- they are stored as plaintext rows. The projected token path is not asked for here: it comes from the deployment, beside the volume it names.`
            : missing.length === 0
              ? "Saving stores the ids. Apply is what makes every node read them."
              : `Still needed: ${missing.map((f) => f.label).join(", ")}. A partial set refuses boot, so it cannot be saved.`}
        </Caption>
      </div>
    </>
  );
}

/**
 * The third door: machines this person owns.
 *
 * NO FORM, because the fix is not here -- pairing a machine is the Fleet app's
 * job, and a second place to do it is a second place to get it wrong. What this
 * panel owes the reader is the state and where to go.
 */
function FleetPanel({ door }: { door: FleetDoor }) {
  return (
    <Panel label="Your machines">
      <div className="os-door-panel">
        <Subhead>Your machines</Subhead>
        <Door
          state={door.state === "open" ? "open" : door.state === "unknown" ? "unknown" : "unset"}
          word={
            door.state === "open"
              ? DOOR_WORDS.open
              : door.state === "unknown"
                ? DOOR_WORDS.unknown
                : DOOR_WORDS.unset
          }
        >
          {door.state === "unknown" && door.error !== ""
            ? "We could not ask this cluster what your machines are offering. That is not the same as a fleet with nothing on it."
            : door.said || "Asking the cluster."}
        </Door>
        {door.error === "" ? null : <p className="os-door-detail os-mono">{door.error}</p>}
        <Caption>
          A machine you own can also run work directly inside a signed-in Claude
          Code or Codex, which spends your subscription rather than this
          cluster&apos;s credit. Pair, label and revoke machines in Fleet, under
          Machines.
        </Caption>
      </div>
    </Panel>
  );
}
