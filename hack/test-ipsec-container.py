#!/usr/bin/env python3
"""Validate IPsec/CNI privilege separation without touching a cluster."""

import os
from pathlib import Path
import shlex
import subprocess
import tempfile

import yaml

ROOT = Path(__file__).resolve().parents[1]


def validate(text, enabled, debug=False):
    documents = [item for item in yaml.safe_load_all(text) if item]
    daemonset = next(item for item in documents if item.get("kind") == "DaemonSet" and item["metadata"]["name"] == "kube-ovn-cni")
    assert daemonset["spec"]["updateStrategy"] == {"type": "RollingUpdate", "rollingUpdate": {"maxSurge": 0, "maxUnavailable": 1}}
    ovs = next(item for item in documents if item.get("kind") == "DaemonSet" and item["metadata"]["name"] == "ovs-ovn")
    ovs_security = ovs["spec"]["template"]["spec"]["containers"][0]["securityContext"]
    assert ovs_security["runAsUser"] == ovs_security["runAsGroup"] == (0 if debug else 65534)
    ovs_pod = ovs["spec"]["template"]["spec"]
    ovs_container = ovs_pod["containers"][0]
    ovs_env = {item["name"]: item for item in ovs_container["env"]}
    assert ovs_env["ENABLE_OVN_IPSEC"]["value"] == str(enabled).lower()
    ovs_mounts = {item["name"]: item for item in ovs_container["volumeMounts"]}
    assert ovs_mounts["ipsec-protection"]["readOnly"] is True
    assert ovs_mounts["ipsec-protection"]["mountPath"] == "/run/kube-ovn-ipsec-protection"
    pod = daemonset["spec"]["template"]["spec"]
    containers = {item["name"]: item for item in pod["containers"]}
    daemon = containers["cni-server"]
    security = daemon["securityContext"]
    assert security["runAsUser"] == (0 if debug else 65534)
    assert "SYS_NICE" not in security["capabilities"]["add"]
    assert "SYS_NICE" in security["capabilities"]["drop"]
    assert all(not arg.startswith(("--enable-ovn-ipsec", "--cert-manager-ipsec-cert", "--ovn-ipsec-cert-duration", "--cert-manager-issuer-name")) for arg in daemon["args"])
    assert all(mount["name"] != "ovs-ipsec-keys" for mount in daemon["volumeMounts"])
    assert all(mount["name"] != "ipsec-protection" for mount in daemon["volumeMounts"])
    for spec in (ovs_pod, pod):
        volume = next(item for item in spec["volumes"] if item["name"] == "ipsec-protection")
        assert volume["hostPath"] == {"path": "/run/kube-ovn-ipsec-protection", "type": "DirectoryOrCreate"}
        init = next(item for item in spec["initContainers"] if item["name"] == "hostpath-init")
        assert f"chown 0:{0 if debug else 65534} /run/kube-ovn-ipsec-protection" in "\n".join(init["command"])
        assert "chmod 0750 /run/kube-ovn-ipsec-protection" in "\n".join(init["command"])
        assert any(item["name"] == "ipsec-protection" for item in init["volumeMounts"])
    assert ("ipsec" in containers) == enabled

    assert "ipsec-cleanup" not in containers
    cleanup = [item for item in pod["initContainers"] if item["name"] == "ipsec-cleanup"]
    assert bool(cleanup) == (not enabled)
    assert any(item["name"] == "ovs-ipsec-keys" for item in pod["volumes"])
    if cleanup:
        agent = cleanup[0]
        assert "restartPolicy" not in agent
        assert agent["image"] == daemon["image"]
        assert agent["command"] == ["/kube-ovn/kube-ovn-ipsec"]
        assert agent["args"] == ["--cleanup-only"]
        assert all(probe not in agent for probe in ("startupProbe", "readinessProbe", "livenessProbe"))
        security = agent["securityContext"]
        assert security["runAsUser"] == 0
        assert security["runAsGroup"] == (0 if debug else 65534)
        assert security["privileged"] is False
        assert security["allowPrivilegeEscalation"] is False
        assert security["capabilities"] == {"drop": ["ALL"], "add": ["NET_ADMIN", "NET_BIND_SERVICE"]}
        mounts = {item["name"]: item for item in agent["volumeMounts"]}
        assert set(mounts) == {"ovs-ipsec-keys", "host-run-ovs", "ipsec-protection"}
        assert mounts["host-run-ovs"]["readOnly"] is True
        env = {item["name"]: item for item in agent["env"]}
        assert env["POD_UID"]["valueFrom"]["fieldRef"]["fieldPath"] == "metadata.uid"
    if enabled:
        ipsec = containers["ipsec"]
        assert ipsec["image"] == daemon["image"]
        assert ipsec["command"] == ["/kube-ovn/kube-ovn-ipsec"]
        security = ipsec["securityContext"]
        assert security["runAsUser"] == 0
        assert security["runAsGroup"] == (0 if debug else 65534)
        assert "--ovn-ipsec-cert-duration=63072000" in ipsec["args"]
        assert security["privileged"] is False
        assert security["allowPrivilegeEscalation"] is False
        assert security["capabilities"] == {"drop": ["ALL"], "add": ["NET_ADMIN", "NET_BIND_SERVICE", "SYS_NICE"]}
        mounts = {item["name"]: item for item in ipsec["volumeMounts"]}
        assert set(mounts) == {"ovs-ipsec-keys", "host-run-ovs", "ipsec-protection"}
        assert mounts["host-run-ovs"]["readOnly"] is True
        env = {item["name"]: item for item in ipsec["env"]}
        assert env["POD_UID"]["valueFrom"]["fieldRef"]["fieldPath"] == "metadata.uid"
        for kind, endpoint in (("startupProbe", "livez"), ("livenessProbe", "livez"), ("readinessProbe", "readyz")):
            assert ipsec[kind]["exec"]["command"] == ["/kube-ovn/kube-ovn-ipsec", f"--check={endpoint}"]


def validate_signer_permissions(text):
    documents = [item for item in yaml.safe_load_all(text) if item]
    controller = next(item for item in documents if item.get("kind") == "ClusterRole" and item["metadata"]["name"] == "system:ovn")
    assert all("secrets" not in rule.get("resources", []) for rule in controller["rules"])
    scoped = next(item for item in documents if item.get("kind") == "Role" and item["metadata"]["name"] == "kube-ovn-controller-secrets")
    assert scoped["metadata"]["namespace"] == "kube-system"
    for rule in scoped["rules"]:
        if "get" in rule["verbs"] or "update" in rule["verbs"]:
            assert set(rule["resourceNames"]) == {"kube-ovn-tls", "ovn-ipsec-ca", "ovn-ipsec-signer"}
    for item in documents:
        if item.get("kind") in ("Role", "ClusterRole") and item["metadata"]["name"] in ("secret-reader-ovn-ipsec", "system:kube-ovn-cni"):
            for rule in item["rules"]:
                if "secrets" in rule.get("resources", []):
                    assert rule["resourceNames"] == ["ovn-ipsec-ca"]


def validate_cert_manager(text):
    documents = [item for item in yaml.safe_load_all(text) if item]
    controller = next(item for item in documents if item.get("kind") == "Deployment" and item["metadata"]["name"] == "kube-ovn-controller")
    assert "--cert-manager-ipsec-cert=true" in controller["spec"]["template"]["spec"]["containers"][0]["args"]
    scoped = next(item for item in documents if item.get("kind") == "Role" and item["metadata"]["name"] == "kube-ovn-controller-secrets")
    assert scoped["metadata"]["namespace"] == "kube-system"
    request_rule = next(rule for rule in scoped["rules"] if rule.get("apiGroups") == ["cert-manager.io"])
    assert request_rule["resources"] == ["certificaterequests"]
    assert set(request_rule["verbs"]) == {"get", "list", "watch", "create"}
    policy = next(item for item in documents if item.get("kind") == "ValidatingAdmissionPolicy")
    binding = next(item for item in documents if item.get("kind") == "ValidatingAdmissionPolicyBinding")
    assert policy["spec"]["failurePolicy"] == "Fail"
    assert policy["spec"]["matchConstraints"]["resourceRules"] == [{"apiGroups": ["cert-manager.io"], "apiVersions": ["v1"], "operations": ["CREATE", "UPDATE"], "resources": ["certificaterequests"]}]
    assert 'object.spec.issuerRef.name == "kube-ovn"' in policy["spec"]["matchConditions"][0]["expression"]
    assert 'request.userInfo.username == "system:serviceaccount:kube-system:ovn"' in policy["spec"]["validations"][0]["expression"]
    assert binding["spec"] == {"policyName": policy["metadata"]["name"], "validationActions": ["Deny"]}
    for item in documents:
        if item.get("kind") in ("Role", "ClusterRole") and item["metadata"]["name"] in ("system:kube-ovn-cni", "secret-reader-ovn-ipsec"):
            assert all("certificaterequests" not in rule.get("resources", []) for rule in item["rules"])


def installer(enabled, debug=False):
    source = (ROOT / "dist/images/install.sh").read_text()
    start = source.index('IPSEC_CONTAINER=""')
    end = source.index("cat <<EOF > kube-ovn.yaml", start)
    rendering = source[start:end]
    start = source.index("kind: DaemonSet", end)
    end = source.index("\n---\nkind: Deployment", start)
    defaults = source[source.index("# debug\n"):source.index("CNI_SERVER_CAPABILITIES=")]
    ovs_start = source.index("kind: DaemonSet\napiVersion: apps/v1\nmetadata:\n  name: ovs-ovn\n")
    ovs_end = source.index("\nEOF", ovs_start)
    script = defaults + "cat <<EOF\n" + source[ovs_start:ovs_end] + "\n---\nEOF\n" + rendering + "cat <<EOF\n" + source[start:end] + "\nEOF\n"
    env = dict(os.environ, ENABLE_OVN_IPSEC=str(enabled).lower(), ENABLE_TPROXY="false",
               REGISTRY="registry.test/kubeovn", VERSION="test", IMAGE_PULL_POLICY="IfNotPresent",
               DEBUG_WRAPPER="valgrind" if debug else "", KUBELET_DIR="/var/lib/kubelet",
               CERT_MANAGER_IPSEC_CERT="false", CERT_MANAGER_ISSUER_NAME="kube-ovn", IPSEC_CERT_DURATION="63072000",
               CNI_SERVER_CAPABILITIES="                - NET_ADMIN\n                - NET_BIND_SERVICE\n                - NET_RAW")
    return subprocess.check_output(["bash", "-c", script], env=env, text=True)


def installer_issuer_policy():
    source = (ROOT / "dist/images/install.sh").read_text()
    start = source.index('IPSEC_ISSUER_POLICY=""')
    end = source.index('IPSEC_CONTAINER=""', start)
    script = 'kubectl() { echo validatingadmissionpolicies.admissionregistration.k8s.io; }\n' + source[start:end] + 'printf "%s\\n" "$IPSEC_ISSUER_POLICY"\n'
    text = subprocess.check_output(["bash", "-c", script], env=dict(os.environ, CERT_MANAGER_IPSEC_CERT="true", CERT_MANAGER_ISSUER_NAME="kube-ovn"), text=True)
    return [item for item in yaml.safe_load_all(text) if item]


def validate_talos_installation():
    # Follow the recursive Make target through install-chart, rather than
    # rendering Helm with values that the real installer never forwards.
    with tempfile.TemporaryDirectory() as directory:
        kubectl = Path(directory) / "kubectl"
        kubectl.write_text("#!/bin/sh\nexit 0\n")
        kubectl.chmod(0o755)
        env = dict(os.environ, PATH=f"{directory}:{os.environ['PATH']}")
        for mode in ("overlay", "underlay"):
            for family in ("ipv4", "ipv6", "dual"):
                target = f"talos-install-{mode}-{family}"
                output = subprocess.check_output(["make", "--no-print-directory", "-n", target], cwd=ROOT, env=env, text=True)
                command = next(line for line in output.replace("\\\n", " ").splitlines() if line.startswith("helm install "))
                args = shlex.split(command)
                assert "OVN_IPSEC_KEY_DIR=/var/lib/ovs_ipsec_keys" in args, f"{target}: IPsec directory was not forwarded to Helm"
                args[1] = "template"
                args.remove("--wait")
                rendered = subprocess.check_output(args, cwd=ROOT, text=True)
                documents = [item for item in yaml.safe_load_all(rendered) if item]
                daemonset = next(item for item in documents if item.get("kind") == "DaemonSet" and item["metadata"]["name"] == "kube-ovn-cni")
                volume = next(item for item in daemonset["spec"]["template"]["spec"]["volumes"] if item["name"] == "ovs-ipsec-keys")
                assert volume["hostPath"]["path"] == "/var/lib/ovs_ipsec_keys"
        output = subprocess.check_output(["make", "--no-print-directory", "-n", "upgrade-chart"], cwd=ROOT,
                                         env=dict(env, OVN_IPSEC_KEY_DIR="/var/lib/ovs_ipsec_keys"), text=True)
        assert "--set OVN_IPSEC_KEY_DIR=/var/lib/ovs_ipsec_keys" in output


def main():
    validate_talos_installation()
    source = (ROOT / "dist/images/install.sh").read_text()
    applied = source.index("kubectl apply -f kube-ovn.yaml")
    for component in ("daemonset/ovs-ovn", "daemonset/kube-ovn-cni"):
        assert source.index(f"kubectl rollout status {component}") > applied, \
            f"{component} must not wait before the IPsec agent or cleanup init is applied"
    for registry in ("", "registry.test"):
        rendered = subprocess.check_output([
            "helm", "template", "hooks", str(ROOT / "charts/kube-ovn-v2"),
            "--set-string", f"global.registry.address={registry}",
            "--set-string", "global.images.kubeovn.repository=ipsec-candidate",
            "--set-string", "global.images.kubeovn.tag=cleanup", "--set", "image.pullPolicy=Never",
        ], text=True, stderr=subprocess.DEVNULL)
        jobs = [item for item in yaml.safe_load_all(rendered) if item and item.get("kind") == "Job" and "helm.sh/hook" in item["metadata"].get("annotations", {})]
        assert len(jobs) == 3
        expected = f"{registry + '/' if registry else ''}ipsec-candidate:cleanup"
        for job in jobs:
            for container in job["spec"]["template"]["spec"]["containers"]:
                assert container["image"] == expected
                assert container["imagePullPolicy"] == "Never"
    for chart, switch, tproxy in (("kube-ovn", "func.ENABLE_OVN_IPSEC", "func.ENABLE_TPROXY"),
                                  ("kube-ovn-v2", "features.enableOvnIpsec", "features.enableTproxy")):
        for enabled in (False, True):
            for tproxy_enabled in (False, True):
                args = ["helm", "template", "ipsec-test", str(ROOT / "charts" / chart), "--set", f"{switch}={str(enabled).lower()}", "--set", f"{tproxy}={str(tproxy_enabled).lower()}"]
                rendered = subprocess.check_output(args, text=True, stderr=subprocess.DEVNULL)
                validate(rendered, enabled)
                validate_signer_permissions(rendered)
        args = ["helm", "template", "ipsec-test", str(ROOT / "charts" / chart), "--set", f"{switch}=true", "--set", "ipsec.certManager.enabled=true"]
        rendered = subprocess.check_output(args, text=True, stderr=subprocess.DEVNULL)
        validate_cert_manager(rendered)
        chart_policy = [item for item in yaml.safe_load_all(rendered) if item and item.get("kind") in ("ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding")]
        assert chart_policy == installer_issuer_policy()
    for enabled in (False, True):
        validate(installer(enabled), enabled)
        validate(installer(enabled, debug=True), enabled, debug=True)
    assert "CAP_SYS_NICE" not in (ROOT / "dist/images/Dockerfile").read_text()
    print("IPsec enabled/disabled, CNI UID, nice capability, mount, probe and installer checks passed.")


if __name__ == "__main__":
    main()
