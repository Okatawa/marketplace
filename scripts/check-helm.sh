#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PWD/.local/bin:$PATH"
mkdir -p .local
helm lint deploy/helm/marketplace
helm template marketplace deploy/helm/marketplace --set ingress.enabled=true --set migrations.enabled=true > .local/helm-rendered.yaml
node tests/browser/check-helm.mjs .local/helm-rendered.yaml
kubeconform -strict -summary -kubernetes-version 1.35.0 .local/helm-rendered.yaml
helm template marketplace deploy/helm/marketplace -f deploy/kind/values.yaml > .local/helm-kind.yaml
kubeconform -strict -summary -kubernetes-version 1.35.0 .local/helm-kind.yaml
helm template marketplace deploy/helm/marketplace --set workloads.enabled=false --set migrations.enabled=true > .local/helm-migrations.yaml
node tests/browser/check-helm.mjs .local/helm-migrations.yaml --migrations-only
kubeconform -strict -summary -kubernetes-version 1.35.0 .local/helm-migrations.yaml
