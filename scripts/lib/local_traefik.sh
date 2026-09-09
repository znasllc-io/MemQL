#!/usr/bin/env bash
# Shared ingress configuration for k3d up and dev (including editor rebuilds).
# Sourcing only defines the helper; callers decide when to change the cluster.

# Traefik's default readTimeout is a 60-second deadline for the ENTIRE request
# body, not an idle timer. A bidirectional gRPC request never finishes its body,
# so client heartbeats cannot prevent it being reset every minute. Both the
# WorkerService and public MemqlService stream use the TLS websecure entrypoint.
# Keep writeTimeout/idleTimeout defaults: neither limits an active request here.
#
# A separate Helm values overlay preserves operator valuesContent, additional
# arguments and other Secret references. Helm merges these two leaf settings;
# it owns the Deployment. Reapplying unchanged values does not roll Traefik.
# Callers source capability.sh first (for cap_json_escape).
function ensure_local_traefik() {
    local cluster="${1:?local cluster name is required}"
    local state version references owned patch
    state="$(kubectl --context "k3d-${cluster}" --request-timeout=30s get helmchartconfig traefik \
        -n kube-system --ignore-not-found \
        -o jsonpath='{.metadata.resourceVersion}{"\n"}{.spec.valuesSecrets}{"\n"}{.spec.valuesSecrets[?(@.name=="memql-local-traefik")].name}')" || return $?
    # The overlay contains no credentials. A Secret is the Helm controller's
    # native values-file mechanism; no host-side YAML parser is required.
    kubectl --context "k3d-${cluster}" --request-timeout=30s apply -f - >&2 <<'VALUES' || return $?
apiVersion: v1
kind: Secret
metadata:
  name: memql-local-traefik
  namespace: kube-system
stringData:
  values.yaml: |-
    providers:
      kubernetesIngress:
        allowExternalNameServices: true
    ports:
      websecure:
        transport:
          respondingTimeouts:
            readTimeout: 0s
VALUES
    if [[ -z "$state" ]]; then
        # Create, rather than apply, so a concurrently created operator config
        # cannot be overwritten. The caller can safely retry on a conflict.
        kubectl --context "k3d-${cluster}" --request-timeout=30s create -f - >&2 <<'CONFIG'
apiVersion: helm.cattle.io/v1
kind: HelmChartConfig
metadata:
  name: traefik
  namespace: kube-system
spec:
  valuesSecrets:
    - name: memql-local-traefik
      keys: [values.yaml]
CONFIG
        return $?
    fi
    {
        IFS= read -r version
        IFS= read -r references || :
        IFS= read -r owned || :
    } <<< "$state"
    [[ "$owned" == "memql-local-traefik" ]] && return 0
    # kubectl serializes this array as JSON. Append without decoding or changing
    # any operator-owned entry; metadata.resourceVersion fences concurrent edits.
    references="${references:-[]}"
    references="${references%]}"
    [[ "$references" == '[' ]] || references+=','
    references+='{"name":"memql-local-traefik","keys":["values.yaml"]}]'
    patch="{\"metadata\":{\"resourceVersion\":\"$(cap_json_escape "$version")\"},\"spec\":{\"valuesSecrets\":${references}}}"
    kubectl --context "k3d-${cluster}" --request-timeout=30s patch helmchartconfig traefik \
        -n kube-system --type=merge -p "$patch" >&2
}
