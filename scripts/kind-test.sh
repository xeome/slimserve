#!/usr/bin/env bash
# Needs docker, kind, kubectl, helm 3.19.0 and kubeconform on PATH. Without ct on PATH, ct runs
# from its image, which bundles yamllint, yamale and ct's lint config. Run from the repository root.
# SKIP_BUILD=1 uses the slimserve:ci image already loaded, as CI does; otherwise it builds one.
set -euo pipefail

# The PID suffix lets runs from two worktrees share a host.
cluster=slimserve-ci-$$
ns=slimserve-ci
image=slimserve:ci
nodist=slimserve-ci-nodist-$$
node_image=kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5
ct_image=quay.io/helmpack/chart-testing:v3.15.0@sha256:54317f2d93730ea9ca1236e3d79cd95d74110b8d733b6ab124e5829209b5f4e4

tmp=$(mktemp -d)
export KUBECONFIG="$tmp/kubeconfig"
install -m 600 /dev/null "$KUBECONFIG"
step=setup
made_cluster=
made_nodist=

on_exit() {
  local status=$?
  if [ "$status" != 0 ]; then
    echo "FAIL at step: $step"
    if [ -s "$KUBECONFIG" ]; then
      echo "== events in $ns"
      kubectl get events -n "$ns" --sort-by=.lastTimestamp || true
      kubectl get events -n s3 --sort-by=.lastTimestamp || true
      for pod in $(kubectl get pods -n "$ns" -o name 2>/dev/null); do
        echo "== logs of $pod"
        kubectl logs -n "$ns" "$pod" --all-containers --tail=30 || true
        echo "== previous logs of $pod"
        kubectl logs -n "$ns" "$pod" --all-containers --tail=30 --previous || true
      done
      echo "== logs of the stand-in S3"
      kubectl logs -n s3 deploy/s3 --tail=30 || true
    fi
  fi
  [ -z "$made_nodist" ] || docker rm -f "$nodist" >/dev/null 2>&1 || true
  [ -z "$made_cluster" ] || kind delete cluster --name "$cluster" --kubeconfig "$KUBECONFIG" >/dev/null 2>&1 || true
  rm -rf "$tmp"
  exit "$status"
}
trap on_exit EXIT

begin() {
  step=$1
  echo "== $step"
}

ct() {
  if type -P ct >/dev/null; then
    command ct "$@"
  else
    # shortcut: here ct runs on the image's helm 3.16.4 and kubectl 1.32, in CI on 3.19.0; mount a static helm if they diverge.
    docker run --rm --network host --user "$(id -u):$(id -g)" -e HOME=/tmp -e KUBECONFIG=/kubeconfig \
      -v "$KUBECONFIG:/kubeconfig:ro" -v "$PWD:/work:ro" -w /work "$ct_image" ct "$@"
  fi
}

rpc_all() {
  local expected=$1 code=$2 pod out
  shift 2
  local pending=("$@")
  for _ in $(seq 1 12); do
    local left=()
    for pod in "${pending[@]}"; do
      out=$(kubectl exec -n "$ns" "$pod" -- /app/bin/slimserve rpc "$code" 2>&1 | tail -n 1 || true)
      [ "$out" = "$expected" ] || left+=("$pod")
    done
    pending=("${left[@]}")
    [ "${#pending[@]}" = 0 ] && return 0
    sleep 5
  done
  for pod in "${pending[@]}"; do
    echo "$pod printed $(kubectl exec -n "$ns" "$pod" -- /app/bin/slimserve rpc "$code" 2>&1 | tail -n 1), not $expected"
  done
  return 1
}

begin image
if [ "${SKIP_BUILD:-}" = 1 ]; then
  echo "SKIP_BUILD=1: using the existing $image"
else
  docker build -q -t "$image" . >/dev/null
fi

begin "A container started with no distribution setting takes no cluster connection"
docker run -d --name "$nodist" -p 127.0.0.1::8080 "$image" >/dev/null
made_nodist=1
port=$(docker port "$nodist" 8080/tcp | head -n 1 | sed 's/.*://')
code=000
for _ in $(seq 1 100); do
  code=$(curl -s -m 2 -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/.app/healthz" || true)
  [ "$code" = 200 ] && break
  sleep 0.3
done
[ "$code" = 200 ] || { echo "/.app/healthz answered $code"; docker logs "$nodist" 2>&1 | tail -n 20; exit 1; }
# /proc/net/tcp*: field 2 is the local address with the port in hex, field 4 the state, 0A for LISTEN. 8080 is 1F90.
rows=$(docker exec "$nodist" cat /proc/net/tcp /proc/net/tcp6 | awk '$4 == "0A" && $2 !~ /:1F90$/')
[ -z "$rows" ] || { echo "LISTEN rows on a port other than 8080:"; echo "$rows"; exit 1; }
docker rm -f "$nodist" >/dev/null
made_nodist=

begin kubeconform
for values in manifests/helm/ci/*-values.yaml; do
  helm template ci manifests/helm -n "$ns" -f "$values" | kubeconform -kubernetes-version 1.37.0 -strict -summary
done

begin "ct lint"
ct lint --config ct.yaml --charts manifests/helm

begin cluster
kind create cluster --name "$cluster" --image "$node_image" --kubeconfig "$KUBECONFIG" --wait 2m
made_cluster=1
kind load docker-image "$image" --name "$cluster"

begin "stand-in S3"
kubectl apply -f scripts/kind/s3.yaml
kubectl rollout status -n s3 deployment/s3 --timeout 2m

begin namespace
kubectl create namespace "$ns"
# enforce rejects only Pods, so a breaking Deployment shows as FailedCreate; warn flags the apply itself.
kubectl label namespace "$ns" pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/enforce-version=latest pod-security.kubernetes.io/warn=restricted

begin "ct install"
ct install --config ct.yaml --charts manifests/helm --namespace "$ns" --skip-clean-up \
  --helm-extra-set-args "--set image.main.repository=slimserve --set image.main.tag=ci --set image.main.pullPolicy=Never"

begin probes
# A wrong path still gets Ready through the HTTPS redirect, but the kubelet logs it. Startup failures are normal.
events='{range .items[*]}{.involvedObject.name}: {.message}{"\n"}{end}'
warned=$(kubectl get events -n "$ns" --field-selector reason=ProbeWarning -o jsonpath="$events")
failed=$(kubectl get events -n "$ns" --field-selector reason=Unhealthy -o jsonpath="$events" | grep -E ': (Readiness|Liveness) probe' || true)
[ -z "$warned$failed" ] || { echo "probe events:"; echo "$warned"; echo "$failed"; exit 1; }

begin pods
releases=$(helm list -n "$ns" -q)
[ "$(wc -w <<<"$releases")" = 2 ] || { echo "releases: $releases"; exit 1; }
all=()
s3_pod=
for release in $releases; do
  read -ra pods <<<"$(kubectl get pods -n "$ns" -l "app=$release-main" -o jsonpath='{.items[*].metadata.name}')"
  [ "${#pods[@]}" = 2 ] || { echo "$release has ${#pods[@]} pods, not 2"; exit 1; }
  all+=("${pods[@]}")
  if helm get values "$release" -n "$ns" -o json | grep -q '"backend":"s3"'; then s3_pod=${pods[0]}; fi
done
[ -n "$s3_pod" ] || { echo "no release has storage.backend s3"; exit 1; }

begin "Erlang cluster"
rpc_all 1 'IO.puts(length(Node.list()))' "${all[@]}"

begin "S3 reachable"
rpc_all true 'IO.puts(match?({:ok, _}, SlimServe.Storage.list([])))' "$s3_pod"

step=done
echo "PASS: both shapes installed, probed, clustered, and S3 answered"
