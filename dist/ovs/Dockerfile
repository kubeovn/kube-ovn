# syntax = docker/dockerfile:experimental
FROM centos:8 as ovs-rpm

RUN yum install dnf-plugins-core -y && \
    yum config-manager --set-enabled PowerTools && \
    yum install python36 git wget openssl-devel gcc \
    make python3-devel openssl-devel kernel-devel kernel-debug-devel \
    autoconf automake rpm-build redhat-rpm-config libtool libcap-ng-devel \
    checkpolicy selinux-policy-devel unbound unbound-devel gcc-c++ \
    desktop-file-utils graphviz groff python3-sphinx -y
COPY build.sh /root/build.sh
RUN bash /root/build.sh
ENV ARCH x86_64
RUN mkdir /rpms/ && \
    cp /ovs/rpm/rpmbuild/RPMS/${ARCH}/* /ovn/rpm/rpmbuild/RPMS/${ARCH}/* /rpms && \
    cd /rpms && rm -f *debug* *docker* *vtep* *ipsec*


FROM centos:8

RUN yum install -y  \
        bind-utils \
        firewalld-filesystem \
        libpcap \
        hostname ethtool \
        iproute nc \
        unbound unbound-devel \
        tcpdump ipset && yum clean all
RUN rpm -ivh --nodeps https://download-ib01.fedoraproject.org/pub/fedora/linux/releases/30/Everything/x86_64/os/Packages/i/iptables-1.8.0-5.fc30.x86_64.rpm
RUN --mount=type=bind,target=/rpms,from=ovs-rpm,source=/rpms rpm -ivh --nodeps /rpms/*.rpm
