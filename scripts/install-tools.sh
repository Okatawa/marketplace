#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in arm64|aarch64) arch=arm64;; x86_64) arch=amd64;; *) exit 1;; esac
mkdir -p .local/bin .local/downloads
base="https://get.helm.sh/helm-v3.22.1-$os-$arch.tar.gz"
curl -fsSL "$base" -o .local/downloads/helm.tar.gz
curl -fsSL "$base.sha256sum" -o .local/downloads/helm.sha256
python3 - <<'PY'
from pathlib import Path
from hashlib import sha256
p = Path(".local/downloads")
assert sha256((p/"helm.tar.gz").read_bytes()).hexdigest() == (p/"helm.sha256").read_text().split()[0], "Helm checksum mismatch"
PY
tar -xzf .local/downloads/helm.tar.gz -C .local/downloads
cp ".local/downloads/$os-$arch/helm" .local/bin/helm
base="https://github.com/kubernetes-sigs/kind/releases/download/v0.33.0/kind-$os-$arch"
curl -fsSL "$base" -o .local/bin/kind
curl -fsSL "$base.sha256sum" -o .local/downloads/kind.sha256
python3 - <<'PY'
from pathlib import Path
from hashlib import sha256
assert sha256(Path(".local/bin/kind").read_bytes()).hexdigest() == Path(".local/downloads/kind.sha256").read_text().split()[0], "kind checksum mismatch"
PY
chmod +x .local/bin/kind .local/bin/helm
GOBIN="$PWD/.local/bin" go install github.com/yannh/kubeconform/cmd/kubeconform@v0.7.0
