# Changelog

The Marketplace renders this file on the extension's page, so it is written for
someone deciding whether to install rather than for someone reading the repo.

## 0.3.1

First published release. The extension has been built and installed from source
throughout its development; this is the first version available from a registry.

**MemQL language support**
- Syntax highlighting, diagnostics, completion, hover and signature help for
  `.memql` files, from a language server bundled with the extension. It needs
  no cluster and no network.

**Local clusters**
- Create, repair, update and uninstall a local MemQL cluster from the
  Deployments panel, with a checklist before each run that says what will
  happen and a record of what did.
- Build node images from your own checkout and roll the cluster onto them.

**Connected clusters**
- Browse the constructs a cluster has loaded, run them, and read the results.

### Platforms

Published for `linux-x64`, `linux-arm64`, `darwin-arm64` and `darwin-x64`.

Language support works on all four. Creating a **local** cluster additionally
needs `linux/amd64` or `darwin/arm64` -- on the other two the installer says so
before it changes anything, and connecting to an existing cluster is unaffected.
