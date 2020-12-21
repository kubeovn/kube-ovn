# syntax = docker/dockerfile:experimental
FROM ubuntu:20.04 as ovs-builder

ARG RPM_ARCH
ARG ARCH
ARG DEBIAN_FRONTEND=noninteractive
ENV SRC_DIR='/usr/src'

RUN apt update && apt install build-essential git libnuma-dev autoconf curl \
    python3 libmnl-dev libpcap-dev libtool libcap-ng-dev libssl-dev pkg-config \
    python3-six libunbound-dev libunwind-dev dh-make fakeroot debhelper dh-python \
    flake8 python3-sphinx graphviz groff -y

RUN cd /usr/src/ && \
    git clone -b branch-2.14 --depth=1 https://github.com/openvswitch/ovs.git && \
    cd ovs && \
    curl https://github.com/alauda/ovs/commit/df1c802f568be2af84aa81372fec46f5b09b4366.patch | git apply && \
    sed -i 's/2.14.1/2.14.0/g' configure.ac && \
    ./boot.sh && \
    rm -rf .git && \
    DATAPATH_CONFIGURE_OPTS='--prefix=/usr --enable-shared' DEB_BUILD_OPTIONS='parallel=8 nocheck' fakeroot debian/rules binary

RUN cd /usr/src/ && git clone -b branch-20.06 --depth=1 https://github.com/ovn-org/ovn.git && \
    cd ovn && \
    curl https://github.com/alauda/ovn/commit/d8253981485e499979cc47c9453b6be8eebe5f0e.patch | git apply --exclude debian/changelog && \
    curl https://github.com/alauda/ovn/commit/f2db72af17f8ad1ea721b1d02e005ed84620fde2.patch | git apply && \
    sed -i 's/OVN/ovn/g' debian/changelog && \
    rm -rf .git && \
    ./boot.sh && \
    DATAPATH_CONFIGURE_OPTS='--prefix=/usr --with-ovs-source=/usr/src/ovs' DEB_BUILD_OPTIONS='parallel=8 nocheck' fakeroot debian/rules binary

RUN mkdir /packages/ && \
     cp /usr/src/libopenvswitch*.deb /packages && \
     cp /usr/src/openvswitch-*.deb /packages && \
     cp /usr/src/python3-openvswitch*.deb /packages && \
     cp /usr/src/ovn-*.deb /packages && \
     cd /packages && rm -f *dbg* *datapath* *docker* *vtep* *ipsec* *test* *dev*

FROM ubuntu:20.04
ARG RPM_ARCH

RUN apt update && apt install ca-certificates python3 hostname libunwind8 netbase \
        ethtool iproute2 ncat libunbound-dev procps libatomic1 kmod \
        tcpdump ipset curl uuid-runtime openssl inetutils-ping \
        logrotate -y --no-install-recommends && \
        rm -rf /var/lib/apt/lists/*

RUN mkdir -p /var/run/openvswitch && \
    mkdir -p /var/run/ovn && \
    mkdir -p /etc/cni/net.d && \
    mkdir -p /opt/cni/bin

ARG ARCH
ENV CNI_VERSION=v0.8.7
RUN curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_VERSION}/cni-plugins-linux-${ARCH}-${CNI_VERSION}.tgz | tar -xz -C . ./loopback ./portmap

ENV KUBE_VERSION="v1.13.2"

RUN curl -L https://dl.k8s.io/${KUBE_VERSION}/kubernetes-client-linux-${ARCH}.tar.gz | tar -xz -C . && cp ./kubernetes/client/bin/kubectl /usr/bin/kubectl \
 && chmod +x /usr/bin/kubectl && rm -rf ./kubernetes

RUN --mount=type=bind,target=/packages,from=ovs-builder,source=/packages  \
    dpkg -i /packages/libopenvswitch*.deb && \
    dpkg -i /packages/openvswitch-*.deb && \
    dpkg -i /packages/python3-openvswitch*.deb &&\
    dpkg -i --ignore-depends=openvswitch-switch,openvswitch-common /packages/ovn-*.deb

COPY *.sh /kube-ovn/
COPY 01-kube-ovn.conflist /kube-ovn/01-kube-ovn.conflist
COPY logrotate/* /etc/logrotate.d/

WORKDIR /kube-ovn

COPY kube-ovn /kube-ovn/kube-ovn
COPY kube-ovn-cmd /kube-ovn/kube-ovn-cmd
RUN ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-controller && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-daemon && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-monitor && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-pinger && \
    ln -s /kube-ovn/kube-ovn-cmd /kube-ovn/kube-ovn-speaker
