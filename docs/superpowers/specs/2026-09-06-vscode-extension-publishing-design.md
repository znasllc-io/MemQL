# Publishing the VS Code Extension -- Design

- **Date:** 2026-09-06
- **Status:** approved (owner, in session; the two open choices -- the target
  list and the `dryRun` default -- were offered and left as proposed)
- **Scope:** a new `.github/workflows/publish-vscode-extension.yml`, a
  `--target` passthrough in `scripts/vscode/package.sh`, a workflow gate in
  `scripts/ci/`, and manifest polish in `editors/vscode/`. No change to what the
  extension DOES.
- **Closes:** memql#5075

## The problem

There is no way to ship the extension to a user. CI packages a `.vsix` on every
relevant pull request (`vscode-extension` runs `package.sh`) and discards it.
The last mile is what is missing, not the build.

This is not a convenience gap. For a local install the extension IS the product:
it carries the install graph, the capability scripts staged into the archive, and
the whole panel. "How does a user get a fixed extension" is the same question as
"how does a user get a fixed installer", and today the answer is that they clone
the repository and run a Makefile target.

Four issues fixed in one session -- memql#5056, #5064, #5071, #5073 -- were all
extension-side, all user-affecting, and none of them can reach a user.

## Decisions

### D1. Tag-driven, and the version is independent of the engine's

Pushing `memql-vscode-v<version>` publishes. `workflow_dispatch` also exists, for
the dry run below. This mirrors `publish-sdk-core.yml`, which is the repository's
existing answer to the same question for `@znasllc-io/memql-sdk-core`.

The version does NOT track the engine release, because coupling them would
assert something false. A user running extension 0.3.2 can install engine
v0.19.x or v0.20.x; those are independent axes and always will be, since the
extension is a CLIENT that must work across a range of engine releases. Tying
the numbers would also force a republish on every engine release -- the
Marketplace refuses a duplicate version, so each engine tag would have to bump
the extension whether or not it changed.

### D2. The version gate runs before anything is packaged

`vsce` takes its version from `package.json`, not from the tag. So a tag reading
`memql-vscode-v0.3.2` against a manifest reading `0.3.1` publishes `0.3.1` --
silently, under a tag that says otherwise.

The workflow refuses that pair up front and names both numbers. It is checked
FIRST because everything after it is expensive and, past the publish step,
irreversible.

### D3. A published version is immutable, so the manual path is a dry run by default

A version cannot be unpublished and re-published. `workflow_dispatch` therefore
carries `dryRun`, defaulting to **true**: the manual lane packages every target
and stops, which is how the lane itself gets tested without spending a version
number. A tag push is the real thing and takes no input.

Defaulting the safe way round is deliberate. The failure it prevents -- a
half-finished workflow burning 0.3.2 on a broken archive -- is unrecoverable,
and the cost of the default being wrong is one re-run with a checkbox.

### D4. One VSIX per platform, because the language server is bundled per platform

`package.sh` builds `memql-lsp` for ONE platform and stages it at
`bin/<node-platform>-<node-arch>/memql-lsp`, which `resolveServerPath` reads at
run time. A single archive built on an ubuntu runner therefore contains only
`bin/linux-x64/`, and every macOS user would install an extension whose language
server does not exist -- silently, because the extension falls back to looking
for `memql-lsp` on `PATH` and simply finds nothing.

The answer is VS Code's platform-specific extensions: one archive per target,
each carrying its own binary, and the registry serves the right one. It is cheap
here for a specific reason -- the LSP builds `CGO_ENABLED=0`, so a single ubuntu
runner cross-compiles every target with no emulation and no second runner.

**The targets are `linux-x64`, `linux-arm64`, `darwin-arm64`, `darwin-x64`.**

That is deliberately WIDER than `SUPPORTED_PLATFORMS` (`linux/amd64`,
`darwin/arm64`) and narrower than everything, because the extension is two
things at once:

- The language server and the remote-cluster surfaces are platform-independent.
- The local-cluster installer is not, and on an unsupported platform
  `detect.sh` already refuses cleanly and says so.

So an Intel Mac gets working language support, working remote-cluster
management, and an honest refusal if they try to create a local cluster. That is
strictly better than the Marketplace telling them the extension does not exist.
`platform.sh` also records that `darwin/amd64` is "one `refresh-tool-pins.sh`
run and one line away", so this is a platform the project expects to support.

**`win32` is excluded.** Not for the same reason: there is no bash, so the
capability-script runner would fail to SPAWN rather than refuse cleanly, and
nothing in the tree has exercised that path. An unsupported-but-honest platform
is worth shipping to; an untested one that fails incoherently is not.

### D5. Package once per target, publish that same file twice

Each matrix row packages one archive and publishes THAT FILE to both registries.
Packaging separately per registry is the obvious shape and is wrong: two
archives built from one commit can still differ (timestamps, dependency
resolution), and then the two registries serve different bytes under one version
with nothing to compare.

Open VSX is included because `scripts/vscode/install.sh` already advertises
`--editor-cmd=cursor` and `codium`, and both editors can only install from Open
VSX. Publishing to the Marketplace alone would leave the editors this repository
already claims to support with no registry path at all.

### D6. `package.sh` gains `--target`, defaulting to empty

It currently builds `args=(package --no-dependencies)` and never passes a
target. The flag is added as a passthrough with an empty default, so
`make vscode-install` and the existing CI packaging step are unchanged: a build
with no target is the universal archive they already produce.

## The gate

`scripts/ci/publish_vscode_workflow_test.go`, in the idiom of the workflow tests
already there. It asserts:

1. The tag pattern and the version-check step agree on the same prefix. They are
   written in two languages in one file and a rename of one is invisible to the
   other.
2. Every matrix target is a value `node_platform()` can actually produce, so a
   target cannot be added that the extension will never look for at run time.
3. Each registry publishes the SAME packaged path -- the property D5 exists for,
   and the one a later edit is most likely to break by adding a second `package`
   call.
4. The dry run guards BOTH publish steps, not just the first.

## What this design does not do

- **It cannot create the credentials.** `VSCE_PAT` (an Azure DevOps token for
  publisher `znasllc`) and `OVSX_PAT` (an Eclipse account plus a one-time
  namespace claim for `znasllc`) are the operator's to create. Until both exist
  the workflow is inert -- it runs and refuses, rather than doing something
  partial.
- **It does not address version skew** between an installed extension and the
  checkout it drives (memql#5076). Publishing makes that mostly self-correcting
  for a user, because updates arrive on their own; it stays permanent on the
  from-source lane, which is developers.
- **It does not add a pre-release channel.** VS Code supports one; nothing has
  asked for it, and it is one input away if that changes.
