# syntax=docker/dockerfile:1
ARG VERSION=latest
ARG BASE_TAG=$VERSION
FROM kubeovn/kube-ovn-base:$BASE_TAG AS setcap

COPY *.sh /kube-ovn/
COPY kubectl-ko /kube-ovn/kubectl-ko
COPY kubectl-ko-node-agent /kube-ovn/kubectl-ko-node-agent
COPY kubectl-ko-legacy /kube-ovn/kubectl-ko-legacy
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist

COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-acl-sample /kube-ovn/kube-ovn-acl-sample
COPY kube-ovn-acl-sample /kube-ovn/kube-ovn-acl-sample-listener
COPY kube-ovn-cmd /kube-ovn/kube-ovn-cmd
COPY kube-ovn-bfdd-supervisor /kube-ovn/kube-ovn-bfdd-supervisor
COPY kube-ovn-daemon /kube-ovn/kube-ovn-daemon
COPY kube-ovn-tproxy /kube-ovn/kube-ovn-tproxy
COPY kube-ovn-controller /kube-ovn/kube-ovn-controller
COPY vpc-egress-gateway-observer /kube-ovn/vpc-egress-gateway-observer
RUN ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-monitor && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-speaker && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-webhook && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-leader-checker && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-ic-controller && \
    ln -s /kube-ovn/kube-ovn-controller /kube-ovn/kube-ovn-pinger && \
    setcap CAP_SYS_ADMIN+ep /kube-ovn/kube-ovn-tproxy && \
    setcap CAP_NET_ADMIN,CAP_NET_RAW+eip /kube-ovn/kube-ovn-acl-sample-listener && \
    setcap CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-cmd && \
    setcap CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-controller && \
    setcap CAP_NET_ADMIN,CAP_NET_RAW,CAP_NET_BIND_SERVICE+eip /kube-ovn/kube-ovn-daemon && \
    setcap CAP_NET_ADMIN+eip /kube-ovn/vpc-egress-gateway-observer

FROM kubeovn/kube-ovn-base:$BASE_TAG

COPY --chmod=0644 logrotate/* /etc/logrotate.d/
COPY grace_stop_ovn_controller /usr/share/ovn/scripts/grace_stop_ovn_controller

COPY --from=setcap /kube-ovn /kube-ovn
# DPDK base images do not include the nftables backend.
RUN if command -v nft >/dev/null 2>&1; then \
        nft_path=$(readlink -f "$(command -v nft)") && \
        test "$(getcap "$nft_path")" = "$nft_path cap_net_admin=eip"; \
    fi
RUN test "$(getcap /kube-ovn/kube-ovn-tproxy)" = "/kube-ovn/kube-ovn-tproxy cap_sys_admin=ep" && \
    test "$(getcap /kube-ovn/kube-ovn)" = "" && \
    test "$(getcap /kube-ovn/kube-ovn-acl-sample)" = "" && \
    test "$(getcap /kube-ovn/kube-ovn-acl-sample-listener)" = "/kube-ovn/kube-ovn-acl-sample-listener cap_net_admin,cap_net_raw=eip" && \
    test "$(getcap /kube-ovn/kube-ovn-cmd)" = "/kube-ovn/kube-ovn-cmd cap_net_bind_service=eip" && \
    test "$(getcap /kube-ovn/kube-ovn-controller)" = "/kube-ovn/kube-ovn-controller cap_net_bind_service,cap_net_raw=eip" && \
    test "$(getcap /kube-ovn/kube-ovn-daemon)" = "/kube-ovn/kube-ovn-daemon cap_net_bind_service,cap_net_admin,cap_net_raw=eip" && \
    test "$(getcap /kube-ovn/vpc-egress-gateway-observer)" = "/kube-ovn/vpc-egress-gateway-observer cap_net_admin=eip"

RUN /kube-ovn/iptables-wrapper-installer.sh --no-sanity-check

WORKDIR /kube-ovn
