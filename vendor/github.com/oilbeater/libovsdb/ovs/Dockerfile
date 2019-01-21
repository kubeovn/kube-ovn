FROM centos:7

RUN yum install -y  \
	PyYAML bind-utils \
	openssl \
	numactl-libs \
	firewalld-filesystem \
	libpcap \
	hostname \
	iproute strace socat dpdk unbound unbound-devel libmlx4 && \
	yum clean all

ARG rpmArch=x86_64
ARG ovsVer=2.10.1
ARG ovsSubVer=1.el7

RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm
RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-ovn-common-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm
RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-ovn-central-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm
RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-ovn-host-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm
RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-ovn-vtep-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm
RUN rpm -i http://cbs.centos.org/kojifiles/packages/openvswitch/${ovsVer}/${ovsSubVer}/${rpmArch}/openvswitch-devel-${ovsVer}-${ovsSubVer}.${rpmArch}.rpm

RUN mkdir -p /var/run/openvswitch
COPY start-ovs.sh /root/start-ovs.sh
CMD ["/bin/bash", "/root/start-ovs.sh"]