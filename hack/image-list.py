#!/usr/bin/env python3
"""List release runtime images using the Helm v2 chart as the source of truth."""

import argparse
from pathlib import Path
import re
import subprocess
import sys

import yaml


ROOT = Path(__file__).resolve().parents[1]


def container_images(value):
    if isinstance(value, dict):
        for key, child in value.items():
            if (key in ("containers", "initContainers", "ephemeralContainers")
                    and isinstance(child, list)):
                for container in child:
                    yield container["image"]
            elif key != "data":
                yield from container_images(child)
    elif isinstance(value, list):
        for child in value:
            yield from container_images(child)


def image_list(version, arch, values=(), overrides=()):
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?", version):
        raise ValueError(f"invalid release version: {version}")
    args = ["helm", "template", "image-list", str(ROOT / "charts/kube-ovn-v2"),
            "--values", "-"]
    # Use published release tags, including the AMD64-only DPDK build, rather
    # than potentially stale chart defaults during release preparation.
    defaults = {
        "global": {"images": {"kubeovn": {"tag": version}}},
        "natGw": {"image": {"tag": version},
                  "bgpSpeaker": {"image": {"tag": version}}},
        "ovsOvn": {"dpdkHybrid": {"tag": f"{version}-dpdk"}},
    }
    for path in values:
        args.extend(["--values", str(path)])
    for override in overrides:
        args.extend(["--set-string", override])
    # ponytail: optional image producers are enabled explicitly; add new
    # component switches here when the chart gains another optional workload.
    for setting in ("bgpSpeaker.enabled=true", "validatingWebhook.enabled=true",
                    "features.enableOvnInterconnections=true",
                    f"ovsOvn.dpdkHybrid.enabled={str(arch == 'amd64').lower()}"):
        args.extend(["--set", setting])
    manifest = subprocess.check_output(args, input=yaml.safe_dump(defaults), text=True)
    images = []
    for document in yaml.safe_load_all(manifest):
        if not document:
            continue
        images.extend(container_images(document))
        # NAT Pods are created by the controller, not by Helm.
        if (document.get("kind") == "ConfigMap"
                and document["metadata"]["name"] == "ovn-vpc-nat-config"):
            data = document["data"]
            images.append(data["image"])
            if data.get("bgpSpeakerImage"):
                images.append(data["bgpSpeakerImage"])
    if not images or any(not isinstance(image, str) or not image
                         or any(char.isspace() for char in image) for image in images):
        raise ValueError("chart produced an empty or invalid runtime image reference")
    return sorted(set(images))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", default=(ROOT / "VERSION").read_text().strip())
    parser.add_argument("--arch", choices=("amd64", "arm64"), default="amd64")
    parser.add_argument("--values", action="append", default=[], help="Helm v2 values file")
    parser.add_argument("--set-string", action="append", default=[], help="Helm image override")
    args = parser.parse_args()
    try:
        images = image_list(args.version, args.arch, args.values, args.set_string)
    except (OSError, subprocess.CalledProcessError, ValueError, yaml.YAMLError) as error:
        parser.exit(1, f"image-list: {error}\n")
    sys.stdout.write("\n".join(images) + "\n")


if __name__ == "__main__":
    main()
