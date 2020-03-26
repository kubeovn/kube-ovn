FROM ovs:latest

RUN mkdir -p /var/run/openvswitch && \
    mkdir -p /var/run/ovn && \
    mkdir -p /etc/cni/net.d && \
    mkdir -p /opt/cni/bin

ENV CNI_VERSION=v0.7.5
RUN curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-amd64-${CNI_VERSION}.tgz | tar -xz -C . ./loopback ./portmap

ENV KUBE_VERSION="v1.13.2"
RUN curl -L https://storage.googleapis.com/kubernetes-release/release/${KUBE_VERSION}/bin/linux/amd64/kubectl -o /usr/bin/kubectl \
 && chmod +x /usr/bin/kubectl

COPY *.sh /kube-ovn/
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist

WORKDIR /kube-ovn

COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-daemon /kube-ovn/kube-ovn-daemon
COPY kube-ovn-pinger /kube-ovn/kube-ovn-pinger
COPY kube-ovn-controller /kube-ovn/kube-ovn-controller
