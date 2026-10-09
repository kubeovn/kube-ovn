#!/usr/bin/env python3
"""Verify node-agent availability in every supported Helm deployment mode."""

from pathlib import Path
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]


def render(chart, mode, pull_secrets):
    args = ["helm", "template", "ko", str(ROOT / "charts" / chart),
            "--set", "MASTER_NODES=192.0.2.10"]
    if mode:
        args.extend(["--set", f"installMode={mode}"])
    if mode == "dataPlaneOnly":
        args.extend(["--set", "externalOvnCentral.nbEndpoint=tcp:192.0.2.10:6641",
                     "--set", "externalOvnCentral.sbEndpoint=tcp:192.0.2.10:6642"])
    for index, name in enumerate(pull_secrets):
        args.extend(["--set-string", f"global.registry.imagePullSecrets[{index}]={name}"])
    manifest = subprocess.check_output(args, text=True)
    return [item for item in yaml.safe_load_all(manifest) if item]


def validate(chart, mode, pull_secrets):
    documents = render(chart, mode, pull_secrets)
    agents = [item for item in documents if item.get("kind") == "DaemonSet"
              and item["metadata"]["name"] == "kubectl-ko-node-agent"]
    assert len(agents) == 1, (chart, mode, "missing or duplicate diagnostics agent")
    pod = agents[0]["spec"]["template"]["spec"]
    assert pod["nodeSelector"]["kubernetes.io/os"] == "linux"
    assert pod["automountServiceAccountToken"] is False
    assert pod["containers"][0]["command"] == ["/kube-ovn/kubectl-ko-node-agent"]
    assert pod.get("imagePullSecrets", []) == [{"name": name} for name in pull_secrets if name]
    if mode:
        # Making diagnostics available must preserve the existing plane split.
        resources = {(item.get("kind"), item["metadata"]["name"]) for item in documents}
        assert (("Deployment", "ovn-central") in resources) == (mode != "dataPlaneOnly")
        assert (("DaemonSet", "kube-ovn-cni") in resources) == (mode != "controlPlaneOnly")
    print(f"PASS {chart} {mode or 'default'} pull secrets={pull_secrets}")


if __name__ == "__main__":
    for chart, modes in [("kube-ovn", ["full", "controlPlaneOnly", "dataPlaneOnly"]),
                         ("kube-ovn-v2", [None])]:
        for mode in modes:
            for pull_secrets in [[], [""], ["registry-pull", "", "registry-mirror-pull"]]:
                validate(chart, mode, pull_secrets)
