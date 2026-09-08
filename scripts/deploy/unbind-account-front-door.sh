#!/usr/bin/env bash
#
# scripts/deploy/unbind-account-front-door.sh
# ===========================================
#
# Capability: frontdoor.unbind -- remove the Certificate and four Ingresses
# that were serving an account's reserved MemQL name.
#
# The mirror of bind-account-front-door.sh (epic memql#5168, design E). Backend
# for the account front-door reconciler's `removing` -> `removed` step, and the
# operator's manual path for the same job.
#
# THE THREE HOSTS HAVE ALREADY STOPPED RESOLVING BY THE TIME THIS RUNS, and
# that ordering is deliberate rather than incidental.
# `requestAccountFrontDoorRemoval` writes status `removing`, and the edge's own
# read (`liveAccountFrontDoorByReservedName`) filters `status=="live"` -- so the
# app. host stops answering at the speed of a row write. This script is the
# cleanup behind that decision, which is why its failure is loud but not
# urgent.
#
# WHY THIS RUNS AT ALL IS WORTH KNOWING (design D9). A front door comes down
# because the RESERVATION behind it went away -- an operator changed the
# client's domain, which clears memqlDomain, the ownership token and the
# verification, or the account was archived. Serving three names whose
# ownership proof has just been discarded is the state this exists to end, so a
# failure here leaves the cluster answering on a name it no longer claims and
# should be read that way rather than as tidying.
#
# THE ROW SURVIVES. Nothing here deletes a graph row and nothing should: how
# long this cluster served a client's name, and when it stopped, is the audit
# trail.
#
# DELETION ORDER IS LOAD-BEARING. The Certificate goes first so cert-manager
# stops renewing before the routes disappear; the reverse leaves a window where
# a renewal races the deletion and can recreate the Secret behind Ingresses
# that are already gone.
#
# ABSENT IS SUCCESS. Unbinding twice is a legitimate thing for a retry to do,
# and the api. HTTP Ingress is legitimately absent on a door that was bound
# with no paths at all -- so a NotFound that failed would make both look like a
# broken cluster.
#
# Refs: memql#5168 memql#4805 memql#2221

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../lib/capability.sh
source "${SCRIPT_DIR}/../lib/capability.sh"

cap_init "frontdoor.unbind" \
    "Remove the Certificate and four Ingresses for an account's reserved MemQL name. Idempotent: absent is success."

cap_spec_param_required "accountId"    "the v1:accounts:account row id -- every object is named after it"
cap_spec_param          "reservedName" "the account's reserved name, recorded in the result so a log line names what came down"
cap_spec_param          "doorId"       "the v1:platform:accountFrontDoor row id, recorded in the result"
cap_spec_param          "namespace"    "namespace the objects live in (default: memql)"
cap_spec_param          "dryRun"       "report what would be removed without removing it"

cap_handle_meta "$@"
cap_parse_flags "$@"

ACCOUNT_ID="$(cap_param accountId "")"
RESERVED_NAME="$(cap_param reservedName "")"
DOOR_ID="$(cap_param doorId "")"
NAMESPACE="$(cap_param namespace "memql")"
DRY_RUN="$(cap_bool_str dryRun false)"

OBJECT_NAME=""
REMOVED=()
ABSENT=()

function check_params() {
    [[ -n "$ACCOUNT_ID" ]] \
        || cap_fail 2 "--accountId is required: every object is named after it, so there is nothing to remove without one"
    local slug
    slug="$(printf '%s' "$ACCOUNT_ID" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-' '-' | sed 's/^-*//; s/-*$//')"
    [[ -n "$slug" ]] || cap_fail 2 "--accountId ${ACCOUNT_ID} contains no usable characters for an object name"
    # Mirrors bind-account-front-door.sh and objectName() in
    # integrations/customdomain/accountdoor_provision.go. Three spellings of
    # one rule is two too many, and account_front_door_test.go pins all three
    # together.
    OBJECT_NAME="account-front-door-${slug:0:200}"
    return 0
}

function check_prereqs() {
    command -v kubectl &>/dev/null \
        || cap_fail 4 "kubectl is not installed or not on PATH"
    if [[ "$DRY_RUN" == "true" ]]; then
        return 0
    fi
    kubectl cluster-info &>/dev/null \
        || cap_fail 4 "no reachable Kubernetes API -- fetch a kubeconfig first"
    return 0
}

# remove_object deletes one object, treating absent as success.
function remove_object() {
    local kind="$1" name="$2"
    if [[ "$DRY_RUN" == "true" ]]; then
        if kubectl get "${kind}/${name}" -n "$NAMESPACE" &>/dev/null; then
            cap_info "dry run: would delete ${kind}/${name} in ${NAMESPACE}"
            REMOVED+=("${kind}/${name}")
        else
            ABSENT+=("${kind}/${name}")
        fi
        return 0
    fi
    local out
    if out="$(kubectl delete "${kind}/${name}" -n "$NAMESPACE" --ignore-not-found 2>&1)"; then
        if [[ -n "$out" ]]; then
            cap_info "$out"
            REMOVED+=("${kind}/${name}")
            cap_changed
        else
            # --ignore-not-found prints nothing when there was nothing to
            # delete, which is exactly the signal wanted here.
            ABSENT+=("${kind}/${name}")
        fi
        return 0
    fi
    cap_fail 5 "could not delete ${kind}/${name} in ${NAMESPACE}: ${out}"
}

function collect_result() {
    cap_result_set "accountId"    "$ACCOUNT_ID"
    cap_result_set "doorId"       "$DOOR_ID"
    cap_result_set "reservedName" "$RESERVED_NAME"
    cap_result_set "namespace"    "$NAMESPACE"
    cap_result_set "objectName"   "$OBJECT_NAME"
    cap_result_set "removed"      "$(IFS=,; printf '%s' "${REMOVED[*]:-}")"
    cap_result_set "absent"       "$(IFS=,; printf '%s' "${ABSENT[*]:-}")"
    return 0
}

function main() {
    check_params
    check_prereqs
    # Certificate first: cert-manager stops renewing before the routes go.
    remove_object "certificate" "${OBJECT_NAME}"
    # All four Ingresses, including the api. HTTP one that is legitimately
    # absent on a door bound with no paths.
    remove_object "ingress" "${OBJECT_NAME}-app"
    remove_object "ingress" "${OBJECT_NAME}-api"
    remove_object "ingress" "${OBJECT_NAME}-api-grpc"
    remove_object "ingress" "${OBJECT_NAME}-id"
    collect_result
    cap_ok
}

main "$@"
