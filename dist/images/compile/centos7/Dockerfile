FROM centos:7

RUN yum install -y gcc elfutils-libelf-devel make perl python3 autoconf automake libtool rpm-build openssl-devel git \
    && git clone -b branch-2.16 --depth=1 https://github.com/openvswitch/ovs.git /ovs/ \
    && yum erase -y git && yum clean all
COPY /*  /fastpath/
WORKDIR /fastpath
