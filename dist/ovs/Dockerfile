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
        numactl-libs \
        firewalld-filesystem \
        iptables \
        libpcap \
        hostname \
        iproute strace socat nc \
        unbound unbound-devel \
        tcpdump ipset && yum clean all
COPY --from=ovs-rpm /rpms/* /rpms/
RUN rpm -ivh --nodeps /rpms/*
