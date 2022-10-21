# syntax = docker/dockerfile:experimental
FROM kubeovn/kube-ovn-base:v1.9.13

ARG ARCH
ENV DUMB_INIT_VERSION="1.2.5"
RUN dump_arch="x86_64"; \
    if [ "$ARCH" = "arm64" ]; then dump_arch="aarch64"; fi; \
    curl -sSf -L --retry 5 -o /usr/bin/dumb-init https://github.com/Yelp/dumb-init/releases/download/v${DUMB_INIT_VERSION}/dumb-init_${DUMB_INIT_VERSION}_${dump_arch} && \
    chmod +x /usr/bin/dumb-init

COPY *.sh /kube-ovn/
COPY kubectl-ko /kube-ovn/kubectl-ko
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist
COPY logrotate/* /etc/logrotate.d/
COPY grace_stop_ovn_controller /usr/share/ovn/scripts/grace_stop_ovn_controller

WORKDIR /kube-ovn

COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-cmd /kube-ovn/kube-ovn-cmd
COPY kube-ovn-webhook /kube-ovn/kube-ovn-webhook
RUN ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-daemon && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-monitor && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-pinger && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-speaker && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller-healthcheck

ENTRYPOINT ["/usr/bin/dumb-init", "--"]
