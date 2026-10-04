#!/usr/bin/env bash
# Disposable kind cluster for kubeon development.
# SAFETY: everything uses the isolated kubeconfig .dev/kubeconfig.
# Never touches ~/.kube/config or $KUBECONFIG.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAME="kubeon-dev"
KCFG="$ROOT/.dev/kubeconfig"
BIN="$ROOT/.dev/bin"
unset KUBECONFIG
mkdir -p "$BIN"

if [[ ! -x "$BIN/kind" ]]; then
  GOBIN="$BIN" go install sigs.k8s.io/kind@v0.30.0
fi
KIND="$BIN/kind"
kc() { kubectl --kubeconfig "$KCFG" --context "kind-$NAME" "$@"; }

case "${1:-up}" in
  up)
    if ! "$KIND" get clusters 2>/dev/null | grep -qx "$NAME"; then
      "$KIND" create cluster --name "$NAME" --kubeconfig "$KCFG" --wait 120s
    fi
    "$KIND" export kubeconfig --name "$NAME" --kubeconfig "$KCFG"
    kc apply -f "$ROOT/testdata/fixtures.yaml"
    # integration tests expect settled fixtures: healthy deployments and the
    # crasher pod already in CrashLoopBackOff (takes a few restarts)
    kc -n demo rollout status deploy/web deploy/jsonlog --timeout 180s
    for _ in $(seq 1 90); do
      if kc -n demo get pods -l app=crasher \
          -o jsonpath='{.items[*].status.containerStatuses[*].state.waiting.reason}' | grep -q CrashLoopBackOff; then
        break
      fi
      sleep 2
    done
    if [[ "${STRESS:-0}" == "1" ]]; then
      kc create ns stress --dry-run=client -o yaml | kc apply -f -
      for i in $(seq 1 2000); do
        echo "---"; echo "apiVersion: v1"; echo "kind: ConfigMap"
        echo "metadata: {name: cm-$i, namespace: stress}"; echo "data: {k: v$i}"
      done | kc apply -f - >/dev/null
    fi
    echo "kubeconfig: $KCFG"
    ;;
  down)
    "$KIND" delete cluster --name "$NAME" --kubeconfig "$KCFG"
    ;;
  kubectl)
    shift; kc "$@"
    ;;
  *) echo "usage: $0 up|down|kubectl ..."; exit 1 ;;
esac
