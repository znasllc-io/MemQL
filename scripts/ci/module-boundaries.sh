#!/usr/bin/env bash
# module-boundaries.sh -- run CI's module-boundaries lane locally.
#
# WHY THIS EXISTS. That lane is invisible to everything a developer runs. Both
# `go build ./...` and `make test` resolve through the go.work workspace, where
# every module can see every other one, so an import that crosses a module
# boundary the wrong way builds, vets, tests and passes review -- and fails in
# CI with a message about a package "no required module provides", which reads
# like a missing `require` rather than a dependency-direction violation.
#
# Epic memql#5137 hit exactly that: `component/router` (a LEAF module the root
# depends on) imported `component/routingrules` (root module). Adding the
# `require` would have made root -> router -> root. The fix was to move the
# shared code down into a package both already depend on, which is a design
# change, and the cheapest moment to learn that is before pushing.
#
# THE WORKFLOW IS THE AUTHORITY, NOT THIS SCRIPT. `.github/workflows/ci.yml`'s
# `module-boundaries` job is what gates a merge, and it does more than this:
# it also checks that go.work matches the modules on disk, that every module's
# go.mod and go.sum are tidy, and that every module is on the workspace's
# version set. The reasoning for each lives in comments there and is not
# repeated here. This script runs the ONE step that catches an import edge,
# because that is the step whose failure is hardest to predict and cheapest to
# reproduce. A green run here is not a green lane; a red run here is a red lane.
#
# Usage:
#   scripts/ci/module-boundaries.sh            # every module
#   scripts/ci/module-boundaries.sh component/router integrations
#
# Exit: 0 when every module builds and vets with GOWORK=off, 1 otherwise.

set -uo pipefail

repo_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
}

# module_dirs prints every directory holding a go.mod, repo-relative, sorted --
# the same derivation ci.yml uses, so the set cannot differ by construction.
module_dirs() {
  find . -name go.mod -not -path './.git/*' -printf '%h\n' | sed 's|^\./||;s|^\.$|.|' | sort
}

check_one() {
  local dir="$1"
  local out
  out="$( cd "$dir" && GOWORK=off go build ./... 2>&1 && GOWORK=off go vet ./... 2>&1 )"
  if [ -n "$out" ]; then
    printf 'FAIL %s\n' "$dir"
    printf '%s\n' "$out" | sed 's/^/     /' | head -12
    return 1
  fi
  printf 'ok   %s\n' "$dir"
  return 0
}

main() {
  cd "$(repo_root)" || exit 1

  local -a dirs=()
  if [ "$#" -gt 0 ]; then
    dirs=("$@")
  else
    # NOT `mapfile`: it is bash 4.0 and macOS ships 3.2, where it fails at
    # RUNTIME rather than at `bash -n`, so it reaches the operator as an abort
    # with no name in it.
    while IFS= read -r line; do dirs+=("$line"); done < <(module_dirs)
  fi

  if [ "${#dirs[@]}" -eq 0 ]; then
    echo "no modules found -- this check examined nothing, which is not a pass" >&2
    exit 1
  fi

  local status=0
  local dir
  for dir in "${dirs[@]}"; do
    check_one "$dir" || status=1
  done

  if [ "$status" -ne 0 ]; then
    cat >&2 <<'EOF'

A module fails to build or vet with GOWORK=off. Two shapes, fixed differently:

  "no required module provides package X"
      An import edge no module boundary allows. Check the DIRECTION before
      adding a require: if the importing module is a leaf the root depends on,
      the require would close a cycle and the fix is to move the shared code
      down into a package both already depend on.

  "updates to go.mod needed"
      A stale go.mod, usually because a sibling this module replaces by
      relative path was bumped (memql#3280). Run:
      cd <dir> && GOWORK=off go mod tidy
EOF
  fi

  printf '\n%d module(s) checked, status=%d\n' "${#dirs[@]}" "$status"
  exit "$status"
}

main "$@"
