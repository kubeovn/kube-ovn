#!/usr/bin/env python3
"""Check optional NetworkManager helper rendering without applying Kubernetes resources."""

import os
from pathlib import Path
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]


def validate(manifest, enabled, tproxy_enabled):
    documents = [item for item in yaml.safe_load_all(manifest) if item]
    cni = next(item for item in documents if item.get("kind") == "DaemonSet" and item["metadata"]["name"] == "kube-ovn-cni")
    pod = cni["spec"]["template"]["spec"]
    containers = {item["name"]: item for item in pod["containers"]}
    daemon = containers["cni-server"]
    assert f"--enable-nm-helper={str(enabled).lower()}" in daemon["args"]
    # The helper exists so that cni-server can stay non-root.
    assert daemon["securityContext"]["runAsUser"] == 65534
    assert ("nm-helper" in containers) == enabled
    assert ("fsGroup" in pod["securityContext"]) == tproxy_enabled
    volumes = {item["name"]: item for item in pod["volumes"]}
    assert ("nm-helper-socket" in volumes) == enabled
    for container in pod.get("initContainers", []) + pod["containers"]:
        mounts = {item["name"] for item in container.get("volumeMounts", [])}
        assert ("nm-helper-socket" in mounts) == (enabled and container["name"] in ("cni-server", "nm-helper"))
    if not enabled:
        return
    helper = containers["nm-helper"]
    assert helper["securityContext"] == {
        "runAsUser": 0,
        "runAsGroup": 65534,
        "privileged": False,
        "allowPrivilegeEscalation": False,
        "capabilities": {"drop": ["ALL"]},
    }
    assert helper["command"] == ["/kube-ovn/kube-ovn-nm-helper"]
    assert helper["image"] == daemon["image"]
    assert helper["imagePullPolicy"] == daemon["imagePullPolicy"]
    assert helper["env"] == [{"name": "DBUS_SYSTEM_BUS_ADDRESS", "value": "unix:path=/host/var/run/dbus/system_bus_socket"}]
    assert {item["name"]: item for item in helper["volumeMounts"]} == {
        "nm-helper-socket": {"name": "nm-helper-socket", "mountPath": "/run/kube-ovn-nm-helper"},
        "host-dbus": {"name": "host-dbus", "mountPath": "/host/var/run/dbus", "readOnly": True, "mountPropagation": "HostToContainer"},
    }
    assert volumes["nm-helper-socket"] == {"name": "nm-helper-socket", "emptyDir": {}}
    assert volumes["host-dbus"]["hostPath"]["path"] == "/var/run/dbus"
    daemon_mounts = {item["name"]: item for item in daemon["volumeMounts"]}
    assert daemon_mounts["nm-helper-socket"]["mountPath"] == "/run/kube-ovn-nm-helper"


def installer_manifest(enabled, tproxy_enabled, ipsec_enabled):
    text = (ROOT / "dist/images/install.sh").read_text()
    start = text.index('IPSEC_CONTAINER=""')
    end = text.index("cat <<EOF > kube-ovn.yaml", start)
    rendering = text[start:end]
    cni_start = text.index("kind: DaemonSet", end)
    cni_end = text.index("\n---\nkind: Deployment", cni_start)
    cni = text[cni_start:cni_end]
    # Supply only interpolation values, then execute the rendering fragment.
    # Do not source install.sh: it invokes kubectl and modifies the cluster.
    script = rendering + "cat <<EOF\n" + cni + "\nEOF\n"
    env = dict(os.environ, ENABLE_NM_HELPER=str(enabled).lower(), ENABLE_TPROXY=str(tproxy_enabled).lower(), REGISTRY="registry.test/kubeovn",
               ENABLE_OVN_IPSEC=str(ipsec_enabled).lower(), IPSEC_CERT_DURATION="63072000", VERSION="test", IMAGE_PULL_POLICY="IfNotPresent", RUN_AS_USER="65534", CNI_RUN_AS_USER="65534", KUBELET_DIR="/var/lib/kubelet",
               CNI_SERVER_CAPABILITIES="                - NET_ADMIN\n                - NET_BIND_SERVICE\n                - NET_RAW")
    return subprocess.check_output(["bash", "-c", script], env=env, text=True)


def main():
    for chart, switch, tproxy_switch, ipsec_switch in (("kube-ovn", "func.ENABLE_NM_HELPER", "func.ENABLE_TPROXY", "func.ENABLE_OVN_IPSEC"),
                                                       ("kube-ovn-v2", "features.enableNmHelper", "features.enableTproxy", "features.enableOvnIpsec")):
        # The switch must default to off.
        validate(subprocess.check_output(["helm", "template", "nm-helper-test", str(ROOT / "charts" / chart)], text=True, stderr=subprocess.DEVNULL), False, False)
        for enabled in (False, True):
            for tproxy_enabled in (False, True):
                command = ["helm", "template", "nm-helper-test", str(ROOT / "charts" / chart), "--set", f"{switch}={str(enabled).lower()}",
                           "--set", f"{tproxy_switch}={str(tproxy_enabled).lower()}"]
                validate(subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL), enabled, tproxy_enabled)
                validate(subprocess.check_output(command + ["--set", f"{ipsec_switch}=true"], text=True, stderr=subprocess.DEVNULL), enabled, tproxy_enabled)
    for enabled in (False, True):
        for tproxy_enabled in (False, True):
            for ipsec_enabled in (False, True):
                validate(installer_manifest(enabled, tproxy_enabled, ipsec_enabled), enabled, tproxy_enabled)
    assert "\nENABLE_NM_HELPER=${ENABLE_NM_HELPER:-false}\n" in (ROOT / "dist/images/install.sh").read_text()
    dockerfile = (ROOT / "dist/images/Dockerfile").read_text()
    assert "COPY --chmod=0111 kube-ovn-nm-helper /kube-ovn/kube-ovn-nm-helper" in dockerfile
    assert 'test "$(getcap /kube-ovn/kube-ovn-nm-helper)" = ""' in dockerfile
    assert not any("setcap" in line and "kube-ovn-nm-helper" in line for line in dockerfile.splitlines())
    build = (ROOT / "makefiles/build.mk").read_text()
    for arch in ("amd64", "arm64"):
        assert f"GOARCH={arch} go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/kube-ovn-nm-helper -v ./cmd/nm_helper" in build
    print("NetworkManager helper enabled/disabled, TProxy/IPsec combinations, installer and image checks passed.")


if __name__ == "__main__":
    main()
