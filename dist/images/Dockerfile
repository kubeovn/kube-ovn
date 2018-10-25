FROM centos:7

ENV PYTHONDONTWRITEBYTECODE yes

RUN yum install -y  \
        PyYAML bind-utils \
        openssl \
        numactl-libs \
        firewalld-filesystem \
        libpcap \
        hostname \
        iproute strace socat \
        unbound unbound-devel && \
        yum clean all

RUN rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-2.10.1-1.el7.centos.x86_64.rpm && \
    rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-devel-2.10.1-1.el7.centos.x86_64.rpm && \
    rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-ovn-common-2.10.1-1.el7.centos.x86_64.rpm && \
    rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-ovn-vtep-2.10.1-1.el7.centos.x86_64.rpm && \
    rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-ovn-central-2.10.1-1.el7.centos.x86_64.rpm && \
    rpm -i https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-ovn-host-2.10.1-1.el7.centos.x86_64.rpm
RUN mkdir -p /var/run/openvswitch && \
    mkdir -p /etc/cni/net.d && \
    mkdir -p /opt/cni/bin
RUN curl -L https://github.com/oilbeater/ovs/releases/download/v2.10.1/openvswitch-kmod-2.10.1-1.el7.centos.x86_64.rpm -o /root/kmod.rpm

COPY start-ovs.sh /root/start-ovs.sh

CMD ["/bin/bash", "/root/start-ovs.sh"]
