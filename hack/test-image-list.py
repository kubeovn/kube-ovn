#!/usr/bin/env python3
"""Check release image inventories against real Helm rendering."""

import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile

import yaml


ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("image_list", ROOT / "hack/image-list.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def check():
    version = "v1.99.7"
    common = [f"docker.io/kubeovn/kube-ovn:{version}",
              f"docker.io/kubeovn/vpc-nat-gateway:{version}"]
    amd64 = sorted(common + [f"docker.io/kubeovn/kube-ovn:{version}-dpdk"])
    assert module.image_list(version, "amd64") == amd64
    assert module.image_list(version, "arm64") == common

    # Unique overrides make omission of an optional workload observable.
    components = ("agent", "bgpSpeaker", "central", "controller", "ic", "monitor",
                  "ovsOvn", "pinger", "validatingWebhook")
    values = {name: {"image": {"registry": "mirror.example:5000",
                              "repository": name.lower(), "tag": "custom"}}
              for name in components}
    values["ovsOvn"]["dpdkHybrid"] = {"enabled": False, "image": {
        "registry": "mirror.example:5000", "repository": "dpdk", "tag": "custom"}}
    values["natGw"] = {"image": {"repository": "mirror.example:5000/nat", "tag": "nat"},
                       "bgpSpeaker": {"image": {
                           "repository": "mirror.example:5000/nat-bgp", "tag": "bgp"}}}
    values["extraObjects"] = [{"apiVersion": "v1", "kind": "Pod",
                               "metadata": {"name": "extra"}, "spec": {
                                   "containers": [{"name": "main", "image": "example/main:1"}],
                                   "initContainers": [{"name": "init", "image": "example/init:1"}]}}]
    # A ConfigMap's arbitrary data is not a container image source.
    values["extraObjects"].append({"apiVersion": "v1", "kind": "ConfigMap",
                                  "metadata": {"name": "unrelated"},
                                  "data": {"image": "example/not-runtime:1"}})
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "values.yaml"
        path.write_text(yaml.safe_dump(values))
        expected = {f"mirror.example:5000/{name.lower()}:custom" for name in components}
        expected.update({f"docker.io/kubeovn/kube-ovn:{version}",
                         "mirror.example:5000/nat:nat", "mirror.example:5000/nat-bgp:bgp",
                         "example/main:1", "example/init:1"})
        assert module.image_list(version, "amd64", [path]) == sorted(
            expected | {"mirror.example:5000/dpdk:custom"})
        assert module.image_list(version, "arm64", [path]) == sorted(expected)
        result = module.image_list(version, "amd64", [path], ["agent.image.tag=override"])
        assert "mirror.example:5000/agent:override" in result
        assert "mirror.example:5000/agent:custom" not in result

    for args in (["--version", "invalid"], ["--arch", "s390x"],
                 ["--values", "/nonexistent/image-list-values.yaml"]):
        result = subprocess.run([sys.executable, str(ROOT / "hack/image-list.py"), *args],
                                capture_output=True, text=True)
        assert result.returncode != 0 and not result.stdout, result

    result = subprocess.check_output(["make", "--no-print-directory", "image-list",
                                      f"IMAGE_LIST_ARGS=--version {version} --arch arm64"],
                                     cwd=ROOT, text=True)
    assert result.splitlines() == common
    print("PASS image inventories, optional components, overrides, architectures and failures")


if __name__ == "__main__":
    check()
