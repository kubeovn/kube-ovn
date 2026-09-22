#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <tag> <output-directory>" >&2
  exit 2
fi

tag=$1
output_dir=$2
repo_root=$(git rev-parse --show-toplevel)

semver_identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
release_tag_regex="^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-$semver_identifier(\.$semver_identifier)*)?$"
if [[ ! $tag =~ $release_tag_regex ]]; then
  echo "invalid release tag: $tag" >&2
  exit 1
fi

chart_version=${tag#v}

mkdir -p "$output_dir"
output_dir=$(cd "$output_dir" && pwd)
workspace=$(mktemp -d)
trap 'rm -rf "$workspace"' EXIT

cp -R "$repo_root/charts" "$workspace/charts"
cp "$repo_root/dist/images/install.sh" "$output_dir/install.sh"
cp "$repo_root/charts/kube-ovn-v2/crds/kube-ovn-crd.yaml" "$output_dir/kube-ovn-crd.yaml"

ruby - "$output_dir/install.sh" "$tag" <<'RUBY'
path, tag = ARGV
content = File.read(path)
registry_changed = content.sub!(/^REGISTRY=.*$/, 'REGISTRY="ghcr.io/qiniu"')
version_changed = content.sub!(/^VERSION=.*$/, "VERSION=\"#{tag}\"")
raise "REGISTRY assignment not found in #{path}" unless registry_changed
raise "VERSION assignment not found in #{path}" unless version_changed
File.write(path, content)
RUBY

ruby -ryaml - "$workspace/charts" "$tag" <<'RUBY'
charts_dir, tag = ARGV
chart_version = tag.delete_prefix("v")

def update_yaml(path)
  values = YAML.load_file(path)
  yield values
  File.write(path, YAML.dump(values))
end

for chart in %w[kube-ovn kube-ovn-v2]
  update_yaml(File.join(charts_dir, chart, "Chart.yaml")) do |values|
    values["version"] = chart_version
    values["appVersion"] = chart_version
  end
end

update_yaml(File.join(charts_dir, "kube-ovn", "values.yaml")) do |values|
  values.fetch("global").fetch("registry")["address"] = "ghcr.io/qiniu"
  values.fetch("global").fetch("images").fetch("kubeovn")["tag"] = tag
  values["DPDK_IMAGE_TAG"] = "#{tag}-dpdk"
end

update_yaml(File.join(charts_dir, "kube-ovn-v2", "values.yaml")) do |values|
  values.fetch("global").fetch("registry")["address"] = "ghcr.io/qiniu"
  values.fetch("global").fetch("images").fetch("kubeovn")["tag"] = tag
  values.fetch("natGw").fetch("image")["repository"] = "ghcr.io/qiniu/vpc-nat-gateway"
  values.fetch("natGw").fetch("image")["tag"] = tag
  values.fetch("natGw").fetch("bgpSpeaker").fetch("image")["repository"] = "ghcr.io/qiniu/kube-ovn"
  values.fetch("natGw").fetch("bgpSpeaker").fetch("image")["tag"] = tag
  values.fetch("ovsOvn").fetch("dpdkHybrid")["tag"] = "#{tag}-dpdk"
end
RUBY

for chart in kube-ovn kube-ovn-v2; do
  helm lint "$workspace/charts/$chart"
  helm template "$chart" "$workspace/charts/$chart" \
    --include-crds \
    --namespace kube-system > "$output_dir/$chart.yaml"
  helm package "$workspace/charts/$chart" --destination "$output_dir"
done

assets=(
  "$output_dir/install.sh"
  "$output_dir/kube-ovn-crd.yaml"
  "$output_dir/kube-ovn.yaml"
  "$output_dir/kube-ovn-v2.yaml"
  "$output_dir/kube-ovn-$chart_version.tgz"
  "$output_dir/kube-ovn-v2-$chart_version.tgz"
)

for asset in "${assets[@]}"; do
  if [[ ! -s $asset ]]; then
    echo "release asset is missing or empty: $asset" >&2
    exit 1
  fi
done

bash -n "$output_dir/install.sh"
ruby -ryaml -e 'ARGV.each { |path| YAML.load_stream(File.read(path)) }' \
  "$output_dir/kube-ovn-crd.yaml" \
  "$output_dir/kube-ovn.yaml" \
  "$output_dir/kube-ovn-v2.yaml"
grep -Fq "ghcr.io/qiniu/kube-ovn:$tag" "$output_dir/kube-ovn.yaml"
grep -Fq "ghcr.io/qiniu/kube-ovn:$tag" "$output_dir/kube-ovn-v2.yaml"
grep -Fq "ghcr.io/qiniu/vpc-nat-gateway:$tag" "$output_dir/kube-ovn-v2.yaml"
