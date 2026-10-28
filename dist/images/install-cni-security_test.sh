#!/usr/bin/env bash
# Match literal shell/template expressions rather than expand them in this test.
# shellcheck disable=SC2016
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
INSTALL_SCRIPT="$SCRIPT_DIR/install.sh"
IMAGE_DOCKERFILE="$SCRIPT_DIR/Dockerfile"

! grep -q 'DISABLE_LEGACY_CNI_EXECUTION' "$INSTALL_SCRIPT"
! grep -q -- '--disable-legacy-cni-execution' "$INSTALL_SCRIPT"
grep -q 'runAsUser: ${RUN_AS_USER}' "$INSTALL_SCRIPT"
cni_server_template=$(sed -n '/^      - name: cni-server$/,/^        env:$/p' "$INSTALL_SCRIPT")
grep -q 'allowPrivilegeEscalation: true' <<<"$cni_server_template"

setcap_stage=$(sed '/^FROM kubeovn\/kube-ovn-base:\$BASE_TAG$/,$d' "$IMAGE_DOCKERFILE")
grep -Fq 'setcap CAP_SYS_ADMIN+ep /kube-ovn/kube-ovn-tproxy' <<<"$setcap_stage"
grep -Fq 'setcap CAP_NET_ADMIN,CAP_NET_RAW+eip /kube-ovn/kube-ovn-acl-sample-listener' <<<"$setcap_stage"
grep -Fq 'COPY kube-ovn-acl-sample /kube-ovn/kube-ovn-acl-sample' <<<"$setcap_stage"
grep -Fq 'COPY kube-ovn-acl-sample /kube-ovn/kube-ovn-acl-sample-listener' <<<"$setcap_stage"
grep -Fq 'setcap CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-cmd' <<<"$setcap_stage"
grep -Fq 'setcap CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-controller' <<<"$setcap_stage"
grep -Fq 'setcap CAP_NET_ADMIN,CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-daemon' <<<"$setcap_stage"
grep -Fq 'setcap CAP_NET_ADMIN+eip /kube-ovn/vpc-egress-gateway-observer' <<<"$setcap_stage"
! grep -Eq 'setcap .*CAP_SYS_ADMIN.*kube-ovn-daemon' <<<"$setcap_stage"

final_stage=$(sed -n '/^FROM kubeovn\/kube-ovn-base:\$BASE_TAG$/,$p' "$IMAGE_DOCKERFILE")
! grep -Eq '(^|[[:space:]])setcap[[:space:]]' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-tproxy)" = "/kube-ovn/kube-ovn-tproxy cap_sys_admin=ep"' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn)" = ""' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-acl-sample)" = ""' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-acl-sample-listener)" = "/kube-ovn/kube-ovn-acl-sample-listener cap_net_admin,cap_net_raw=eip"' <<<"$final_stage"
! grep -Fq 'ln -s /kube-ovn/kube-ovn /kube-ovn/kube-ovn-acl-sample' <<<"$setcap_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-cmd)" = "/kube-ovn/kube-ovn-cmd cap_net_bind_service=eip"' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-controller)" = "/kube-ovn/kube-ovn-controller cap_net_bind_service,cap_net_raw=eip"' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/kube-ovn-daemon)" = "/kube-ovn/kube-ovn-daemon cap_net_bind_service,cap_net_admin,cap_net_raw=eip"' <<<"$final_stage"
grep -Fq 'test "$(getcap /kube-ovn/vpc-egress-gateway-observer)" = "/kube-ovn/vpc-egress-gateway-observer cap_net_admin=eip"' <<<"$final_stage"

# Evaluate only the complete UID/capability initialization block. Never source
# the whole installer or a fixed line prefix that can cut a multiline string.
grep -q '^# debug$' "$INSTALL_SCRIPT"
grep -q '^KUBELET_DIR=' "$INSTALL_SCRIPT"
for ipsec_enabled in false true; do
  new_config=$(
    ENABLE_OVN_IPSEC="$ipsec_enabled" DEBUG_WRAPPER="" bash -c '
      set -euo pipefail
      source <(sed -n "/^# debug$/,/^KUBELET_DIR=/{ /^KUBELET_DIR=/q; p; }" "$1")
      printf "%s\n%s" "$RUN_AS_USER" "$CNI_SERVER_CAPABILITIES"
    ' bash "$INSTALL_SCRIPT"
  )
  new_user=${new_config%%$'\n'*}
  new_caps=${new_config#*$'\n'}
  [[ "$new_user" == 65534 ]]
  [[ "$new_caps" != *SYS_ADMIN* ]]
  [[ "$new_caps" != *SYS_PTRACE* ]]
  [[ "$new_caps" != *SYS_NICE* ]]
done

echo 'install.sh CNI security checks passed'
