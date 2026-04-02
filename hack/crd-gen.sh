#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."

CONTROLLER_GEN_BIN="${GOBIN:-$(go env GOPATH)/bin}/controller-gen"
CONTROLLER_TOOLS_VERSION=${CONTROLLER_TOOLS_VERSION:-"v0.20.1"}
go install sigs.k8s.io/controller-tools/cmd/controller-gen@"${CONTROLLER_TOOLS_VERSION}"

# Clear old generated CRDs to avoid duplicate files.
mkdir -p ./yamls/gen
rm -f ./yamls/gen/*.yaml

"${CONTROLLER_GEN_BIN}" crd:allowDangerousTypes=true paths=./pkg/apis/kubeovn/v1 output:crd:artifacts:config=./yamls/gen

python - <<'PY'
from pathlib import Path
import re
import yaml

repo = Path('.')
gen_dir = repo / 'yamls' / 'gen'
crd_files = sorted(p for p in gen_dir.glob('*.yaml') if p.name != 'kube-ovn-crd.yaml')
if not crd_files:
    raise SystemExit('no generated CRD files found under yamls/gen')

# Normalize bundle order by CRD metadata.name for deterministic output.
docs = []
for path in crd_files:
    data = yaml.safe_load(path.read_text())
    if not isinstance(data, dict) or data.get('kind') != 'CustomResourceDefinition':
        raise SystemExit(f'unexpected generated CRD document in {path}')
    docs.append((data['metadata']['name'], data))
docs.sort(key=lambda item: item[0])

parts = []
for _, doc in docs:
    parts.append(yaml.safe_dump(doc, sort_keys=False).rstrip())

bundle = '\n---\n'.join(parts) + '\n'

# Write generated bundle for reference/use by downstream tooling.
(repo / 'yamls' / 'gen' / 'kube-ovn-crd.yaml').write_text(bundle)

# Sync Helm static CRD bundles.
(repo / 'charts' / 'kube-ovn' / 'templates' / 'kube-ovn-crd.yaml').write_text(bundle)
(repo / 'charts' / 'kube-ovn-v2' / 'crds' / 'kube-ovn-crd.yaml').write_text(bundle)

# Sync embedded install.sh CRD heredoc.
install_path = repo / 'dist' / 'images' / 'install.sh'
install_text = install_path.read_text()
pattern = re.compile(r'cat <<EOF > kube-ovn-crd\.yaml\n.*?\nEOF\n', re.S)
replacement = 'cat <<EOF > kube-ovn-crd.yaml\n' + bundle + 'EOF\n'
new_text, count = pattern.subn(lambda _m: replacement, install_text, count=1)
if count != 1:
    raise SystemExit('failed to replace kube-ovn-crd.yaml heredoc in dist/images/install.sh')
install_path.write_text(new_text)
PY
