FROM centos:8

COPY /  /fastpath/
RUN find /etc/yum.repos.d/ -type f -exec sed -i 's/mirrorlist=/#mirrorlist=/g' {} + \
    && find /etc/yum.repos.d/ -type f -exec sed -i 's/#baseurl=/baseurl=/g' {} + \
    && find /etc/yum.repos.d/ -type f -exec sed -i 's/mirror.centos.org/vault.centos.org/g' {} + \
    && yum install -y gcc elfutils-libelf-devel make perl python3 autoconf automake libtool rpm-build openssl-devel git \
    && git clone -b branch-2.16 --depth=1 https://github.com/openvswitch/ovs.git /ovs/ \
    && yum erase -y git && yum clean all
COPY /  /fastpath/
RUN rm -f /fastpath/kube_ovn_fastpath.c && mv /fastpath/4.18/kube_ovn_fastpath.c /fastpath/kube_ovn_fastpath.c
WORKDIR /fastpath
