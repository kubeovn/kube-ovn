#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
INSTALL_SCRIPT="$SCRIPT_DIR/install.sh"
IMAGE_DOCKERFILE="$SCRIPT_DIR/Dockerfile"

! grep -q 'DISABLE_LEGACY_CNI_EXECUTION' "$INSTALL_SCRIPT"
! grep -q -- '--disable-legacy-cni-execution' "$INSTALL_SCRIPT"
grep -q 'runAsUser: ${RUN_AS_USER}' "$INSTALL_SCRIPT"
cni_server_template=$(sed -n '/^      - name: cni-server$/,/^        env:$/p' "$INSTALL_SCRIPT")
grep -q 'allowPrivilegeEscalation: true' <<<"$cni_server_template"

final_stage=$(sed -n '/^FROM kubeovn\/kube-ovn-base:\$BASE_TAG$/,$p' "$IMAGE_DOCKERFILE")
grep -q 'setcap CAP_SYS_ADMIN+ep /kube-ovn/kube-ovn-tproxy' <<<"$final_stage"
grep -q 'getcap /kube-ovn/kube-ovn-tproxy).*cap_sys_admin=ep' <<<"$final_stage"
grep -q 'setcap CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-cmd' <<<"$final_stage"
grep -q 'setcap CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-controller' <<<"$final_stage"
grep -q 'setcap CAP_NET_ADMIN,CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-daemon' <<<"$final_stage"
grep -q 'setcap CAP_NET_ADMIN+eip /kube-ovn/vpc-egress-gateway-observer' <<<"$final_stage"
! grep -Eq 'setcap .*CAP_SYS_ADMIN.*kube-ovn-daemon' <<<"$final_stage"

# Evaluate the installer inputs and the generated capability selection without
# running the installer (which applies resources to the current Kubernetes
# context). ENABLE_IC is supplied so the prefix does not probe kubectl.
new_config=$(
  ENABLE_IC=false bash -c '
    source <(sed -n "1,130p" "$1")
    printf "%s\n%s" "$RUN_AS_USER" "$CNI_SERVER_CAPABILITIES"
  ' bash "$INSTALL_SCRIPT"
)
new_user=${new_config%%$'\n'*}
new_caps=${new_config#*$'\n'}
[[ "$new_user" == 65534 ]]
[[ "$new_caps" != *SYS_ADMIN* ]]
[[ "$new_caps" != *SYS_PTRACE* ]]

echo 'install.sh CNI security checks passed'
