#!/usr/bin/env bash
set -euo pipefail

# Install an immutable bundle before publishing the CNI entrypoint. Tools run
# with the image's ELF loader and dependency closure, independently of host ABI.
cni_source=$1
cni_target=$2
bundle_root="${cni_target%/*}/.kube-ovn-bundles"
mkdir -p "$bundle_root"
bundle=$(mktemp -d "$bundle_root/bundle.XXXXXXXX")
mkdir -p "$bundle/tools/bin" "$bundle/tools/lib"
cp "$cni_source" "$bundle/kube-ovn"

for tool in ovs-vsctl ethtool; do
  source_path=$(command -v "$tool")
  cp -L "$source_path" "$bundle/tools/bin/$tool"
  dependencies=$(ldd "$source_path" 2>&1) || {
    echo "Cannot resolve dependencies of $tool: $dependencies" >&2
    exit 1
  }
  if [[ "$dependencies" == *"not found"* ]]; then
    echo "Missing library dependency of $tool: $dependencies" >&2
    exit 1
  fi
  while IFS= read -r library; do
    cp -L "$library" "$bundle/tools/lib/${library##*/}"
  done < <(awk '{for (i=1;i<=NF;i++) if ($i ~ /^\//) print $i}' <<<"$dependencies")
  loader=$(awk '$1 ~ /^\/.*\/ld-linux[^\/]*$/ {print ($2 == "=>" ? $3 : $1)}' <<<"$dependencies")
  if [[ -z "$loader" || "$loader" == *$'\n'* ]]; then
    echo "Cannot identify the ELF loader for $tool" >&2
    exit 1
  fi
  cat > "$bundle/tools/$tool" <<EOF
#!/bin/sh
tool_dir=\${0%/*}
exec "\$tool_dir/lib/${loader##*/}" --library-path "\$tool_dir/lib" "\$tool_dir/bin/$tool" "\$@"
EOF
  chmod 755 "$bundle/tools/$tool"
  "$bundle/tools/$tool" --version > /dev/null
done

# Keep prior bundles for in-flight ADD/DEL processes and operator rollback.
# The host CNI directory may be mounted at a different path in the installer.
ln -s ".kube-ovn-bundles/${bundle##*/}/kube-ovn" "${cni_target}.bundle.$$"
mv -Tf "${cni_target}.bundle.$$" "$cni_target"
