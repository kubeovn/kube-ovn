#!/usr/bin/env python3
"""Check optional privileged helpers without applying Kubernetes resources."""

import os
from pathlib import Path
import subprocess

import yaml


ROOT = Path(__file__).resolve().parents[1]


def validate(manifest, enabled, node_gateway_enabled=False):
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
    assert ("fsGroup" in pod["securityContext"]) == (enabled or node_gateway_enabled)
    if enabled or node_gateway_enabled:
        assert pod["securityContext"]["fsGroup"] == 65534
    validate_node_gateway(pod, node_gateway_enabled)
    for init in pod.get("initContainers", []):
        assert all(mount["name"] != "tproxy-socket" for mount in init.get("volumeMounts", []))
    if enabled:
        helper = containers["tproxy"]
        security = helper["securityContext"]
        assert security["runAsUser"] == 65534
        assert security["runAsNonRoot"] is True
        assert security["runAsGroup"] == 0
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


def validate_node_gateway(pod, enabled):
    containers = {item["name"]: item for item in pod["containers"]}
    volumes = {item["name"]: item for item in pod["volumes"]}
    assert ("node-gateway-helper" in containers) == enabled
    assert ("node-gateway-host-mnt" in volumes) == enabled
    assert ("node-gateway-socket" in volumes) == enabled
    daemon = containers["cni-server"]
    assert f"--enable-node-gateway-helper={str(enabled).lower()}" in daemon["args"]
    assert not {"SYS_ADMIN", "SYS_CHROOT"} & set(daemon["securityContext"]["capabilities"]["add"])
    daemon_mounts = {item["name"]: item for item in daemon["volumeMounts"]}
    assert ("node-gateway-socket" in daemon_mounts) == enabled
    for container in pod["containers"] + pod.get("initContainers", []):
        if container["name"] != "node-gateway-helper":
            assert all(mount["name"] != "node-gateway-host-mnt" for mount in container.get("volumeMounts", []))
    if not enabled:
        return
    helper = containers["node-gateway-helper"]
    security = helper["securityContext"]
    assert security["runAsUser"] == 65534
    assert security["runAsGroup"] == 65534
    assert security["runAsNonRoot"] is True
    assert security["privileged"] is False
    assert security["allowPrivilegeEscalation"] is False
    assert security["capabilities"]["drop"] == ["ALL"]
    assert set(security["capabilities"]["add"]) == {"SYS_ADMIN", "SYS_CHROOT", "NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE"}
    assert helper["command"] == ["/kube-ovn/kube-ovn-node-gw"]
    assert volumes["node-gateway-host-mnt"]["hostPath"] == {"path": "/proc/1/ns/mnt", "type": "File"}
    mounts = {item["name"]: item for item in helper["volumeMounts"]}
    assert mounts["node-gateway-host-mnt"] == {"name": "node-gateway-host-mnt", "mountPath": "/run/kube-ovn-node-gw-host-mnt", "readOnly": True}
    assert mounts["host-ns"]["mountPropagation"] == "HostToContainer"
    assert pod["hostPID"] is True


def installer_manifest(enabled, run_as_user=65534, node_gateway_enabled=False):
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
    env = dict(os.environ, ENABLE_TPROXY=str(enabled).lower(), ENABLE_NODE_GATEWAY_HELPER=str(node_gateway_enabled).lower(),
               REGISTRY="registry.test/kubeovn", VERSION="test",
               IMAGE_PULL_POLICY="IfNotPresent", RUN_AS_USER=str(run_as_user), KUBELET_DIR="/var/lib/kubelet",
               CNI_SERVER_CAPABILITIES="                - NET_ADMIN\n                - NET_BIND_SERVICE\n                - NET_RAW")
    return subprocess.check_output(["bash", "-c", script], env=env, text=True)


def main():
    for chart, switch, ipsec_switch in (("kube-ovn", "func.ENABLE_TPROXY", "func.ENABLE_OVN_IPSEC"),
                                         ("kube-ovn-v2", "features.enableTproxy", "features.enableOvnIpsec")):
        for enabled in (False, True):
            command = ["helm", "template", "tproxy-test", str(ROOT / "charts" / chart), "--set", f"{switch}={str(enabled).lower()}"]
            validate(subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL), enabled)
            validate(subprocess.check_output(command + ["--set", f"{ipsec_switch}=true"], text=True, stderr=subprocess.DEVNULL), enabled)
            if chart == "kube-ovn-v2":
                validate(subprocess.check_output(command + ["--set", "ovsOvn.dpdkHybrid.enabled=true"], text=True, stderr=subprocess.DEVNULL), enabled)
    for enabled in (False, True):
        for run_as_user in (0, 65534):
            validate(installer_manifest(enabled, run_as_user), enabled)
    for node_gateway_enabled in (False, True):
        for enabled in (False, True):
            for chart, switch, gateway_switch in (("kube-ovn", "func.ENABLE_TPROXY", "func.ENABLE_NODE_GATEWAY_HELPER"),
                                                    ("kube-ovn-v2", "features.enableTproxy", "features.enableNodeGatewayHelper")):
                command = ["helm", "template", "helper-test", str(ROOT / "charts" / chart),
                           "--set", f"{switch}={str(enabled).lower()}",
                           "--set", f"{gateway_switch}={str(node_gateway_enabled).lower()}"]
                validate(subprocess.check_output(command, text=True, stderr=subprocess.DEVNULL), enabled, node_gateway_enabled)
            for run_as_user in (0, 65534):
                validate(installer_manifest(enabled, run_as_user, node_gateway_enabled), enabled, node_gateway_enabled)
    for name in ("start-ovs.sh", "start-ovs-dpdk-v2.sh"):
        assert "kube-ovn-tproxy" not in (ROOT / "dist/images" / name).read_text()
    dockerfile = (ROOT / "dist/images/Dockerfile").read_text()
    assert "setcap CAP_SYS_ADMIN+ep /kube-ovn/kube-ovn-tproxy" in dockerfile
    assert 'getcap /kube-ovn/kube-ovn-tproxy)" = "/kube-ovn/kube-ovn-tproxy cap_sys_admin=ep"' in dockerfile
    assert "setcap CAP_SYS_ADMIN,CAP_SYS_CHROOT,CAP_NET_ADMIN,CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-node-gw" in dockerfile
    assert 'getcap /kube-ovn/kube-ovn-node-gw)" = "/kube-ovn/kube-ovn-node-gw cap_net_bind_service,cap_net_admin,cap_net_raw,cap_sys_chroot,cap_sys_admin=eip"' in dockerfile
    print("TProxy/node gateway enabled/disabled, DPDK, installer and capability separation checks passed.")


if __name__ == "__main__":
    main()
