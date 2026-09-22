#!/usr/bin/env bash
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
output_dir=$(mktemp -d)
trap 'rm -rf "$output_dir"' EXIT

ruby -ryaml - "$repo_root/.github/workflows/publish-images.yaml" <<'RUBY'
workflow = YAML.load_file(ARGV.fetch(0))
jobs = workflow.fetch("jobs")
raise "tagged releases must not rebuild base images" if jobs.key?("build-base-images")

prepare_job = jobs.fetch("prepare")
unless prepare_job.fetch("outputs").fetch("base_tag") == "${{ steps.release.outputs.base_tag }}"
  raise "prepare must expose the base image tag"
end
prepare = prepare_job.fetch("steps").find { |step| step["name"] == "Validate release tag" }
unless prepare&.fetch("run")&.include?("base_tag=$(<VERSION)")
  raise "prepare must read the base image tag from VERSION"
end

build_job = jobs.fetch("build-release-images")
unless build_job.fetch("needs") == "prepare"
  raise "build-release-images must only depend on prepare"
end

steps = build_job.fetch("steps")
setup_buildx = steps.find { |step| step["uses"] == "docker/setup-buildx-action@v3" }
unless setup_buildx&.dig("with", "driver") == "docker"
  raise "build-release-images must use the docker driver to export local images"
end

build = steps.find { |step| step["name"] == "Build release images" }
raise "Build release images step is missing" unless build
unless build.fetch("env").fetch("BASE_TAG") == "${{ needs.prepare.outputs.base_tag }}"
  raise "Build release images must use the base tag from VERSION"
end

command = build.fetch("run")
unless command.include?("make -f Makefile.release") && command.include?("release-images")
  raise "Build release images must build the Qiniu x86 image set"
end
for target in %w[base-amd64 base-amd64-dpdk image-kube-ovn-debug image-kube-ovn-dpdk image-vpc-nat-gateway]
  raise "Build release images must not run #{target}" if command.match?(/(^|\s)#{Regexp.escape(target)}(\s|$)/)
end

release_steps = jobs.fetch("release").fetch("steps")
publish_release = release_steps.find { |step| step["name"] == "Create or update GitHub release" }
raise "Create or update GitHub release step is missing" unless publish_release
release_command = publish_release.fetch("run")
unless release_command.include?('gh release create "$TAG"') && release_command.include?('"${release_assets[@]}"')
  raise "immutable releases must be created with their assets"
end
if release_command.include?('gh release upload')
  raise "immutable releases must not upload assets after publication"
end
RUBY

make -n -f "$repo_root/Makefile.release" \
  RELEASE_TAG=v1.15.10-alpha.1 \
  BASE_TAG=v1.15.10 \
  release-images | grep -Fq -- '--build-arg BASE_TAG=v1.15.10'

for invalid_tag in \
  release-1.15.10-alpha.1 \
  v1.15.10.. \
  v1.15.10-alpha..1 \
  v1.15.10-. \
  v01.15.10 \
  v1.15.10-01; do
  if "$repo_root/hack/package-release.sh" "$invalid_tag" "$output_dir/invalid" 2>/dev/null; then
    echo "package-release.sh accepted invalid release tag $invalid_tag" >&2
    exit 1
  fi
done

for tag in v1.15.10 v1.15.10-alpha.1 v1.15.10-rc.1 v1.15.10-qiniu.1 v1.15.11-alpha.1; do
  tag_output_dir="$output_dir/${tag#v}"
  "$repo_root/hack/package-release.sh" "$tag" "$tag_output_dir"

  grep -Fq 'REGISTRY="ghcr.io/qiniu"' "$tag_output_dir/install.sh"
  grep -Fq "VERSION=\"$tag\"" "$tag_output_dir/install.sh"
  tar -xOf "$tag_output_dir/kube-ovn-${tag#v}.tgz" kube-ovn/Chart.yaml |
    grep -Fq "version: ${tag#v}"
  tar -xOf "$tag_output_dir/kube-ovn-${tag#v}.tgz" kube-ovn/Chart.yaml |
    grep -Fq "appVersion: ${tag#v}"
  tar -xOf "$tag_output_dir/kube-ovn-${tag#v}.tgz" kube-ovn/values.yaml |
    grep -Fq 'address: ghcr.io/qiniu'
  tar -xOf "$tag_output_dir/kube-ovn-${tag#v}.tgz" kube-ovn/values.yaml |
    grep -Fq "DPDK_IMAGE_TAG: $tag-dpdk"
  tar -xOf "$tag_output_dir/kube-ovn-v2-${tag#v}.tgz" kube-ovn-v2/Chart.yaml |
    grep -Fq "version: ${tag#v}"
  tar -xOf "$tag_output_dir/kube-ovn-v2-${tag#v}.tgz" kube-ovn-v2/Chart.yaml |
    grep -Fq "appVersion: ${tag#v}"
  tar -xOf "$tag_output_dir/kube-ovn-v2-${tag#v}.tgz" kube-ovn-v2/values.yaml |
    grep -Fq 'repository: ghcr.io/qiniu/vpc-nat-gateway'
  tar -xOf "$tag_output_dir/kube-ovn-v2-${tag#v}.tgz" kube-ovn-v2/values.yaml |
    grep -Fq "tag: $tag-dpdk"
done
