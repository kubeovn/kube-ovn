FROM centos:7

ENV PYTHONDONTWRITEBYTECODE yes

RUN yum install -y  \
        PyYAML bind-utils \
        openssl \
        numactl-libs \
        firewalld-filesystem \
        libpcap \
        hostname \
        iproute strace socat nc \
        unbound unbound-devel \
        tcpdump ipset \
        epel-release https://centos7.iuscommunity.org/ius-release.rpm

RUN yum install python34 -y && ln -s /bin/python3.4 /bin/python3 && yum clean all

ENV OVS_VERSION=20.03.0
ENV OVS_SUBVERSION=1

RUN rpm -ivh --nodeps https://github.com/alauda/ovs/releases/download/${OVS_VERSION}-${OVS_SUBVERSION}/openvswitch-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh --nodeps https://github.com/alauda/ovs/releases/download/${OVS_VERSION}-${OVS_SUBVERSION}/openvswitch-devel-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh --nodeps https://github.com/alauda/ovs/releases/download/${OVS_VERSION}-${OVS_SUBVERSION}/ovn-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh --nodeps https://github.com/alauda/ovs/releases/download/${OVS_VERSION}-${OVS_SUBVERSION}/ovn-central-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm && \
    rpm -ivh --nodeps https://github.com/alauda/ovs/releases/download/${OVS_VERSION}-${OVS_SUBVERSION}/ovn-host-${OVS_VERSION}-${OVS_SUBVERSION}.el7.x86_64.rpm

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
