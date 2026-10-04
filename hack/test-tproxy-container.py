#!/usr/bin/env python3
"""Check optional TProxy rendering without applying Kubernetes resources."""

import os
from pathlib import Path
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]


def validate(manifest, enabled):
    documents = [item for item in yaml.safe_load_all(manifest) if item]
    cni = next(item for item in documents if item.get("kind") == "DaemonSet" and item["metadata"]["name"] == "kube-ovn-cni")
    pod = cni["spec"]["template"]["spec"]
    containers = {item["name"]: item for item in pod["containers"]}
    daemon = containers["cni-server"]
    assert f"--enable-tproxy={str(enabled).lower()}" in daemon["args"]
    assert ("tproxy" in containers) == enabled
    volumes = {item["name"]: item for item in pod["volumes"]}
    assert ("tproxy-socket" in volumes) == enabled
    daemon_mounts = {item["name"]: item for item in daemon["volumeMounts"]}
    assert ("tproxy-socket" in daemon_mounts) == enabled
    for init in pod.get("initContainers", []):
        assert all(mount["name"] != "tproxy-socket" for mount in init.get("volumeMounts", []))
    if enabled:
        helper = containers["tproxy"]
        security = helper["securityContext"]
        assert security["runAsUser"] == 0
        assert security["runAsGroup"] == 65534
        assert security["privileged"] is False
        assert security["allowPrivilegeEscalation"] is False
        assert security["capabilities"] == {"drop": ["ALL"], "add": ["SYS_ADMIN"]}
        assert helper["command"] == ["/kube-ovn/kube-ovn-tproxy"]
        assert helper["image"] == daemon["image"]
        assert volumes["tproxy-socket"] == {"name": "tproxy-socket", "emptyDir": {}}
        mounts = {item["name"]: item for item in helper["volumeMounts"]}
        assert set(mounts) == {"tproxy-socket", "host-run-ovs", "host-ns"}
        assert mounts["host-run-ovs"]["readOnly"] is True
        assert mounts["host-ns"]["readOnly"] is True
        assert mounts["host-ns"]["mountPropagation"] == "HostToContainer"
        assert mounts["tproxy-socket"]["mountPath"] == daemon_mounts["tproxy-socket"]["mountPath"] == "/run/kube-ovn-tproxy"
    for item in documents:
        if item.get("kind") != "DaemonSet" or not item["metadata"]["name"].startswith("ovs-ovn"):
            continue
        ovs_pod = item["spec"]["template"]["spec"]
        assert all(volume["name"] != "host-netns" for volume in ovs_pod["volumes"])
        for container in ovs_pod["containers"]:
            assert all(mount["mountPath"] != "/var/run/netns" for mount in container.get("volumeMounts", []))


def installer_manifest(enabled):
    text = (ROOT / "dist/images/install.sh").read_text()
    start = text.index('TPROXY_CONTAINER=""')
    end = text.index("cat <<EOF > kube-ovn.yaml", start)
    rendering = text[start:end]
    cni_start = text.index("kind: DaemonSet", end)
    cni_end = text.index("\n---\nkind: Deployment", cni_start)
    cni = text[cni_start:cni_end]
    # Supply only interpolation values, then execute the rendering fragment.
    # Do not source install.sh: it invokes kubectl and modifies the cluster.
    script = rendering + "cat <<EOF\n" + cni + "\nEOF\n"
    env = dict(os.environ, ENABLE_TPROXY=str(enabled).lower(), REGISTRY="registry.test/kubeovn", VERSION="test",
               IMAGE_PULL_POLICY="IfNotPresent", RUN_AS_USER="65534", KUBELET_DIR="/var/lib/kubelet",
               CNI_SERVER_CAPABILITIES="                - NET_ADMIN\n                - NET_BIND_SERVICE\n                - NET_RAW")
    return subprocess.check_output(["bash", "-c", script], env=env, text=True)


def main():
    for chart, switch in (("kube-ovn", "func.ENABLE_TPROXY"), ("kube-ovn-v2", "features.enableTproxy")):
        for enabled in (False, True):
            command = ["helm", "template", "tproxy-test", str(ROOT / "charts" / chart), "--set", f"{switch}={str(enabled).lower()}"]
            validate(subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL), enabled)
            if chart == "kube-ovn-v2":
                validate(subprocess.check_output(command + ["--set", "ovsOvn.dpdkHybrid.enabled=true"], text=True, stderr=subprocess.DEVNULL), enabled)
    for enabled in (False, True):
        validate(installer_manifest(enabled), enabled)
    for name in ("start-ovs.sh", "start-ovs-dpdk-v2.sh"):
        assert "kube-ovn-tproxy" not in (ROOT / "dist/images" / name).read_text()
    dockerfile = (ROOT / "dist/images/Dockerfile").read_text()
    assert not any("setcap" in line and "kube-ovn-tproxy" in line for line in dockerfile.splitlines())
    print("TProxy enabled/disabled, DPDK, installer and capability separation checks passed.")


if __name__ == "__main__":
    main()
