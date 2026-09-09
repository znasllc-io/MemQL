#!/usr/bin/env bash
# Local build/import integrity shared by the k3d development path.
# Bash 3.2; no host-side JSON parser or containerd client is required.

function local_image_id_valid() {
    [[ "$1" =~ ^sha256:[0-9a-f]{64}$ ]]
}

# Capture the build's own result, rather than inspecting a mutable tag after
# another editor/terminal build could have replaced it. Caller owns build args.
function build_local_image() {
    local state rc
    state="$(mktemp -d "${TMPDIR:-/tmp}/memql-image-build.XXXXXX")" || return 1
    if docker build --iidfile "${state}/id" "$@" >&2; then
        LOCAL_IMAGE_BUILT_ID="$(cat "${state}/id")"
        rm -rf "$state"
        if ! local_image_id_valid "$LOCAL_IMAGE_BUILT_ID"; then
            printf 'ERROR: Docker build did not report a valid image ID.\n' >&2
            return 1
        fi
    else
        rc=$?
        rm -rf "$state"
        return "$rc"
    fi
}

function local_image_matches() {
    local image="$1" expected="$2" actual
    if ! local_image_id_valid "$expected"; then
        printf 'ERROR: No valid captured image ID for %s.\n' "$image" >&2
        return 1
    fi
    actual="$(docker image inspect --format '{{.Id}}' "$image")" || return 1
    if [[ "$actual" != "$expected" ]]; then
        printf 'ERROR: %s changed since this build (%s -> %s). Another update may be running; retry after it finishes.\n' "$image" "$expected" "$actual" >&2
        return 1
    fi
}

# Query actual containers, not a guessed server-0 or the configured node count.
# Include stopped nodes: success requires every server and agent to hold the
# image. A missing/unreadable inventory cannot prove an import succeeded.
function local_image_nodes() {
    local cluster="$1" inventory name role state found=0
    inventory="$(docker ps --all --filter "label=k3d.cluster=${cluster}" \
        --format '{{.Names}} {{.Label "k3d.role"}} {{.State}}')" || return 1
    while read -r name role state; do
        case "$role" in server|agent) ;; *) continue ;; esac
        if [[ "$state" != running ]]; then
            printf 'ERROR: Cluster node %s is %s; its image cannot be verified.\n' "$name" "$state" >&2
            return 1
        fi
        printf '%s\n' "$name"
        found=1
    done <<< "$inventory"
    if [[ "$found" == 0 ]]; then
        printf 'ERROR: No server or agent containers found for k3d cluster %s.\n' "$cluster" >&2
        return 1
    fi
}

function local_image_canonical_ref() {
    local image="$1" first
    # Docker's local refs may omit docker.io and the library namespace.
    if [[ "$image" != */* ]]; then image="docker.io/library/${image}"; fi
    first="${image%%/*}"
    case "$first" in *.*|*:*|localhost) ;; *) image="docker.io/${image}" ;; esac
    if [[ "$image" == docker.io/* && "${image#docker.io/}" != */* ]]; then
        image="docker.io/library/${image#docker.io/}"
    fi
    printf '%s\n' "$image"
}

function import_local_image_verified() {
    local image="$1" cluster="$2" expected="$3" nodes after node canonical listing target config ready
    local_image_matches "$image" "$expected" || return 1
    nodes="$(local_image_nodes "$cluster")" || return 1
    canonical="$(local_image_canonical_ref "$image")"
    # A tools-node import returned success after ctr reported a short read.
    # Direct mode avoids the shared tarball; verify actual runtime state too.
    k3d image import "$image" --cluster "$cluster" --mode direct >&2 || return 1
    local_image_matches "$image" "$expected" || return 1
    after="$(local_image_nodes "$cluster")" || return 1
    if [[ "$nodes" != "$after" ]]; then
        printf 'ERROR: Cluster nodes changed during import; retry against the current cluster.\n' >&2
        return 1
    fi
    while read -r node; do
        listing="$(docker exec "$node" ctr -n k8s.io images ls "name==${canonical}")" || return 1
        target="$(printf '%s\n' "$listing" | awk -v ref="$canonical" '$1 == ref { print $3 }')"
        config="$(docker exec "$node" crictl inspecti -o go-template --template '{{.status.id}}' "$canonical")" || return 1
        # Classic Docker uses a config ID; Docker's containerd image store
        # uses the manifest/index digest. Neither is a tag or a partial hash.
        if ! local_image_id_valid "$target" || ! local_image_id_valid "$config" || \
            [[ "$expected" != "$target" && "$expected" != "$config" ]]; then
            printf 'ERROR: %s on %s does not match built image %s (target %s, config %s).\n' "$image" "$node" "$expected" "$target" "$config" >&2
            return 1
        fi
        ready="$(docker exec "$node" ctr -n k8s.io images check --quiet "name==${canonical}")" || return 1
        if [[ "$ready" != "$canonical" ]]; then
            printf 'ERROR: %s is not fully downloaded and unpacked on %s.\n' "$image" "$node" >&2
            return 1
        fi
        printf 'INFO: Verified %s on %s (%s).\n' "$image" "$node" "$expected" >&2
    done <<< "$nodes"
}
