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

GEN_DIR="yamls/gen"
BUNDLE_FILE="yamls/gen/kube-ovn-crd.yaml"

rm -f "$BUNDLE_FILE"

# Extract metadata.name and sort
for file in "$GEN_DIR"/*.yaml; do
    if [ ! -f "$file" ] || [[ "$file" == "$BUNDLE_FILE" ]]; then
        continue
    fi
    # Use grep to safely extract metadata.name
    name=$(grep -E "^  name: " "$file" | head -n1 | awk '{print $2}' | tr -d '"'"'")
    echo "$name $file"
done | sort | awk '{print $2}' > /tmp/crd_files_sorted.txt

# Concatenate files with '---' separator
> "$BUNDLE_FILE"
first=1
while read -r file; do
    if [ "$first" -eq 1 ]; then
        cat "$file" >> "$BUNDLE_FILE"
        first=0
    else
        echo "---" >> "$BUNDLE_FILE"
        cat "$file" >> "$BUNDLE_FILE"
    fi
done < /tmp/crd_files_sorted.txt

# Sync to other locations
CHART_V1_CRD_PATH="charts/kube-ovn/templates/kube-ovn-crd.yaml"
CHART_V2_CRD_PATH="charts/kube-ovn-v2/crds/kube-ovn-crd.yaml"
INSTALL_PATH="dist/images/install.sh"

cp "$BUNDLE_FILE" "$CHART_V1_CRD_PATH"
cp "$BUNDLE_FILE" "$CHART_V2_CRD_PATH"

START_ANCHOR="# BEGIN GENERATED KUBE-OVN CRD BUNDLE"
END_ANCHOR="# END GENERATED KUBE-OVN CRD BUNDLE"

awk -v start="$START_ANCHOR" -v end="$END_ANCHOR" -v bundle="$BUNDLE_FILE" '
    $0 ~ start {
        print
        print "cat <<'"'"'EOF'"'"' > kube-ovn-crd.yaml"
        while ((getline line < bundle) > 0) {
            print line
        }
        print "EOF"
        print end
        skip = 1
        next
    }
    $0 ~ end {
        skip = 0
        next
    }
    !skip { print }
' "$INSTALL_PATH" > "${INSTALL_PATH}.tmp" && mv "${INSTALL_PATH}.tmp" "$INSTALL_PATH"

echo "Generated CRDs synced successfully."
