#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD/.local/bin:$PATH"
cluster=marketplace-example
export KUBECONFIG="$PWD/.local/kubeconfig"
make setup
if ! kind get clusters | grep -qx "$cluster"; then
  kind create cluster --name "$cluster" --image kindest/node:v1.37.0 --config deploy/kind/config.yaml --kubeconfig "$KUBECONFIG"
fi
# Never use the user's current kubectl context.
k=(kubectl --kubeconfig "$KUBECONFIG" --context "kind-$cluster" -n marketplace)
kubectl --kubeconfig "$KUBECONFIG" --context "kind-$cluster" create namespace marketplace --dry-run=client -o yaml | kubectl --kubeconfig "$KUBECONFIG" --context "kind-$cluster" apply -f -
arch=$("${k[@]}" get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')
archive=$(mktemp "$PWD/.local/kind-image.XXXXXX.tar")
trap 'rm -f "$archive"' EXIT
load_image() {
  # Docker Desktop may retain a multi-platform index with only one platform's
  # blobs. Export the node's platform explicitly so kind can import every blob.
  docker image save --platform "linux/$arch" "$1" -o "$archive"
  kind load image-archive "$archive" --name "$cluster"
}
for name in gateway accounts listings search messaging notifications migrate; do
  load_image "marketplace/$name:dev"
done
for image in postgres:17.6-alpine apache/kafka:4.1.1 rustfs/rustfs:1.0.0 axllent/mailpit:v1.27.8; do
  docker image inspect "$image" >/dev/null 2>&1 || docker pull "$image"
  load_image "$image"
done
python3 scripts/kind-secrets.py
for path in .local/kind-secrets/*.env; do
  name=$(basename "$path" .env)
  "${k[@]}" create secret generic "$name" --from-env-file="$path" --dry-run=client -o yaml | "${k[@]}" apply -f -
done
"${k[@]}" create configmap postgres-init --from-file=init.sh=deploy/compose/init-postgres.sh --dry-run=client -o yaml | "${k[@]}" apply -f -
if [[ $("${k[@]}" get job kafka-init -o 'jsonpath={.status.conditions[?(@.type=="Failed")].status}' --ignore-not-found) == True ]]; then
  "${k[@]}" delete job kafka-init
fi
"${k[@]}" apply -f deploy/kind/dependencies.yaml
for name in postgres kafka storage mailpit; do
  "${k[@]}" rollout status "deployment/$name" --timeout=240s
done
"${k[@]}" wait --for=condition=complete job/kafka-init --timeout=240s
run_id="local-$(date +%s)"
helm template marketplace deploy/helm/marketplace -f deploy/kind/values.yaml --set workloads.enabled=false --set migrations.enabled=true --set "migrations.runID=$run_id" | "${k[@]}" apply -f -
# Failed Jobs from previous runs remain for diagnostics until their TTL expires.
# They must not block this run after its migration succeeds.
for name in accounts listings search messaging notifications; do
  "${k[@]}" wait --for=condition=complete "job/marketplace-migrate-$name-$run_id" --timeout=300s
done
helm upgrade --install marketplace deploy/helm/marketplace -f deploy/kind/values.yaml --kube-context "kind-$cluster" -n marketplace --wait --timeout=5m
# Local tags are mutable; loading a new :dev image must also replace running pods.
"${k[@]}" rollout restart deployment -l app.kubernetes.io/instance=marketplace
for name in gateway accounts listings search messaging notifications; do
  "${k[@]}" rollout status "deployment/marketplace-$name" --timeout=180s
done
printf 'kind ready: http://127.0.0.1:18080\n'
