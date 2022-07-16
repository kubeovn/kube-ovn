# syntax = docker/dockerfile:experimental
FROM kubeovn/kube-ovn-base:v1.11.0

COPY *.sh /kube-ovn/
COPY kubectl-ko /kube-ovn/kubectl-ko
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist
COPY logrotate/* /etc/logrotate.d/
COPY grace_stop_ovn_controller /usr/share/ovn/scripts/grace_stop_ovn_controller

WORKDIR /kube-ovn

COPY kube-ovn-cmd /kube-ovn/kube-ovn-cmd
COPY kube-ovn-webhook /kube-ovn/kube-ovn-webhook
RUN ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-daemon && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-monitor && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-pinger && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-speaker && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller-healthcheck && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-leader-checker
