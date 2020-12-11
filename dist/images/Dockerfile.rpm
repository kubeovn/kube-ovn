# syntax = docker/dockerfile:experimental
FROM centos:8 as ovs-rpm
ENV BUILD_DATE 20200930
ARG RPM_ARCH
ARG ARCH

RUN yum install dnf-plugins-core -y && \
    yum config-manager --set-enabled powertools && \
    yum install python36 git wget openssl-devel gcc \
    make python3-devel openssl-devel kernel-devel kernel-debug-devel \
    autoconf automake rpm-build redhat-rpm-config libtool libcap-ng-devel \
    checkpolicy selinux-policy-devel unbound unbound-devel gcc-c++ \
    desktop-file-utils graphviz groff python3-sphinx -y
COPY build.sh /root/build.sh
RUN bash /root/build.sh && \
    mkdir /rpms/ && \
    cp /ovs/rpm/rpmbuild/RPMS/${RPM_ARCH}/* /ovn/rpm/rpmbuild/RPMS/${RPM_ARCH}/* /rpms && \
    cd /rpms && rm -f *debug* *docker* *vtep* *ipsec* && \
    rm -rf /ovs /ovn /jemalloc-5.2.1


FROM centos:8
ENV BUILD_DATE 20200930
RUN yum remove -y bind-export-libs && yum update -y && \
    yum install -y \
        firewalld-filesystem openssl \
        libatomic \
        libpcap \
        hostname ethtool \
        iproute nc \
        unbound-devel \
        tcpdump ipset logrotate && \
    yum clean all
ARG RPM_ARCH
RUN rpm -ivh --nodeps https://archives.fedoraproject.org/pub/archive/fedora/linux/releases/30/Everything/${RPM_ARCH}/os/Packages/i/iptables-1.8.0-5.fc30.${RPM_ARCH}.rpm

RUN mkdir -p /var/run/openvswitch && \
    mkdir -p /var/run/ovn && \
    mkdir -p /etc/cni/net.d && \
    mkdir -p /opt/cni/bin

ARG ARCH
ENV CNI_VERSION=v0.8.6
RUN curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-linux-${ARCH}-${CNI_VERSION}.tgz | tar -xz -C . ./loopback ./portmap

ENV KUBE_VERSION="v1.13.2"
RUN curl -L https://storage.googleapis.com/kubernetes-release/release/${KUBE_VERSION}/bin/linux/${ARCH}/kubectl -o /usr/bin/kubectl \
 && chmod +x /usr/bin/kubectl

RUN --mount=type=bind,target=/rpms,from=ovs-rpm,source=/rpms rpm -ivh --nodeps /rpms/*.rpm && \
    sed '/su ovn ovn/d' /etc/logrotate.d/ovn

COPY *.sh /kube-ovn/
COPY grace_stop_ovn_controller /usr/share/ovn/scripts/grace_stop_ovn_controller
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist
RUN rpm -e --nodeps sqlite-libs

WORKDIR /kube-ovn
COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-cmd /kube-ovn/kube-ovn-cmd
RUN ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-daemon && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-monitor && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-pinger && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-speaker
