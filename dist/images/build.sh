#!/usr/bin/env bash
set -euo pipefail

wget https://github.com/jemalloc/jemalloc/archive/5.2.1.tar.gz
tar -zxvf 5.2.1.tar.gz
cd jemalloc-5.2.1
./autogen.sh
make dist
make && make install
echo "/usr/local/lib" > /etc/ld.so.conf.d/other.conf
/sbin/ldconfig
cd ..

git clone -b branch-2.13 --depth=1 https://github.com/openvswitch/ovs.git
cd ovs
curl https://github.com/alauda/ovs/commit/238003290766808ba310e1875157b3d414245603.patch | git apply
sed -i 's/2.13.1/2.13.0/g' configure.ac
sed -i 's/sphinx-build-3/sphinx-build/g' rhel/openvswitch-fedora.spec.in
./boot.sh
./configure LIBS=-ljemalloc
make rpm-fedora
cd ..

git clone -b branch-20.03 --depth=1 https://github.com/ovn-org/ovn.git
cd ovn
curl https://github.com/alauda/ovn/commit/19e802b80c866089af8f7a21512f68decc75a874.patch | git apply
sed -i 's/20.03.1/20.03.0/g' configure.ac
./boot.sh
./configure LIBS=-ljemalloc --with-ovs-source=/ovs
make rpm-fedora
