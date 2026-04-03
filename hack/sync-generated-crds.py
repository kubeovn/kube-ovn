#!/usr/bin/env python3
from __future__ import annotations

import re
from pathlib import Path

import yaml


REPO = Path(__file__).resolve().parent.parent
GEN_DIR = REPO / "yamls" / "gen"
INSTALL_PATH = REPO / "dist" / "images" / "install.sh"
CHART_V1_CRD_PATH = REPO / "charts" / "kube-ovn" / "templates" / "kube-ovn-crd.yaml"
CHART_V2_CRD_PATH = REPO / "charts" / "kube-ovn-v2" / "crds" / "kube-ovn-crd.yaml"
GENERATED_BUNDLE_PATH = GEN_DIR / "kube-ovn-crd.yaml"
START_ANCHOR = "# BEGIN GENERATED KUBE-OVN CRD BUNDLE\n"
END_ANCHOR = "# END GENERATED KUBE-OVN CRD BUNDLE\n"


def load_generated_crd_docs() -> list[dict]:
    crd_files = sorted(path for path in GEN_DIR.glob("*.yaml") if path.name != GENERATED_BUNDLE_PATH.name)
    if not crd_files:
        raise SystemExit("no generated CRD files found under yamls/gen")

    docs: list[tuple[str, dict]] = []
    for path in crd_files:
        data = yaml.safe_load(path.read_text())
        if not isinstance(data, dict) or data.get("kind") != "CustomResourceDefinition":
            raise SystemExit(f"unexpected generated CRD document in {path}")
        docs.append((data["metadata"]["name"], data))

    docs.sort(key=lambda item: item[0])
    return [doc for _, doc in docs]


def render_crd_bundle(docs: list[dict]) -> str:
    parts = [yaml.safe_dump(doc, sort_keys=False).rstrip() for doc in docs]
    return "\n---\n".join(parts) + "\n"


def sync_chart_crd_bundles(bundle: str) -> None:
    GENERATED_BUNDLE_PATH.write_text(bundle)
    CHART_V1_CRD_PATH.write_text(bundle)
    CHART_V2_CRD_PATH.write_text(bundle)


def sync_install_script(bundle: str) -> None:
    install_text = INSTALL_PATH.read_text()
    replacement = (
        START_ANCHOR
        + "cat <<EOF > kube-ovn-crd.yaml\n"
        + bundle
        + "EOF\n"
        + END_ANCHOR
    )

    start = install_text.find(START_ANCHOR)
    end = install_text.find(END_ANCHOR)
    if start != -1 and end != -1 and end > start:
        end += len(END_ANCHOR)
        new_text = install_text[:start] + replacement + install_text[end:]
    else:
        pattern = re.compile(r"cat <<EOF > kube-ovn-crd\.yaml\n.*?\nEOF\n", re.S)
        new_text, count = pattern.subn(lambda _m: replacement, install_text, count=1)
        if count != 1:
            raise SystemExit("failed to replace kube-ovn-crd.yaml block in dist/images/install.sh")

    INSTALL_PATH.write_text(new_text)


def main() -> None:
    docs = load_generated_crd_docs()
    bundle = render_crd_bundle(docs)
    sync_chart_crd_bundles(bundle)
    sync_install_script(bundle)


if __name__ == "__main__":
    main()
