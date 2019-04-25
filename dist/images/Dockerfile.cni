FROM centos:7

RUN yum install -y \
        PyYAML \
        bind-utils \
        openssl \
        numactl-libs \
        firewalld-filesystem \
        libpcap \
        hostname \
        ipset \
        iproute strace socat nc \
        unbound unbound-devel python-openvswitch libreswan && \
        yum clean all

ENV OVS_VERSION=2.11.1
ENV OVS_SUBVERSION=1
ENV CNI_VERSION=v0.7.5

RUN curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-amd64-${CNI_VERSION}.tgz | tar -xz -C . ./loopback

RUN rpm -ivh https://github.com/alauda/ovs/releases/download/v${OVS_VERSION}-${OVS_SUBVERSION}/openvswitch-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh https://github.com/alauda/ovs/releases/download/v${OVS_VERSION}-${OVS_SUBVERSION}/openvswitch-ipsec-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh https://github.com/alauda/ovs/releases/download/v${OVS_VERSION}-${OVS_SUBVERSION}/ovn-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh https://github.com/alauda/ovs/releases/download/v${OVS_VERSION}-${OVS_SUBVERSION}/ovn-common-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh https://github.com/alauda/ovs/releases/download/v${OVS_VERSION}-${OVS_SUBVERSION}/ovn-host-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm

COPY start-cniserver.sh /kube-ovn/start-cniserver.sh
COPY install-cni.sh /kube-ovn/install-cni.sh
COPY kube-ovn.conflist /kube-ovn/kube-ovn.conflist

WORKDIR /kube-ovn
CMD ["sh", "start-cniserver.sh"]

COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-daemon /kube-ovn/kube-ovn-daemon
