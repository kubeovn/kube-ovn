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

git clone -b branch-2.14 --depth=1 https://github.com/openvswitch/ovs.git
cd ovs
# change compact interval to reduce resource usage
curl https://github.com/alauda/ovs/commit/238003290766808ba310e1875157b3d414245603.patch | git apply
sed -i 's/2.14.1/2.14.0/g' configure.ac
sed -i 's/sphinx-build-3/sphinx-build/g' rhel/openvswitch-fedora.spec.in
./boot.sh
if [ "$ARCH" = "amd64" ]; then
  ./configure LIBS=-ljemalloc CFLAGS="-O2 -g -msse4.2 -mpopcnt"
else
  ./configure LIBS=-ljemalloc
fi
make rpm-fedora
cd ..

git clone -b branch-20.06 --depth=1 https://github.com/ovn-org/ovn.git
cd ovn

# kube-ovn related patches
curl https://github.com/alauda/ovn/commit/1aa47202c3e09f610ebefe00ab23a0723059bbf3.patch | git apply
curl https://github.com/alauda/ovn/commit/b75eb1f231da55d1b8a29f386dc56234e2eb9d35.patch | git apply

sed -i 's/20.06.3/20.06.2/g' configure.ac
./boot.sh
if [ "$ARCH" = "amd64" ]; then
  ./configure LIBS=-ljemalloc --with-ovs-source=/ovs CFLAGS="-O2 -g -msse4.2 -mpopcnt"
else
  ./configure LIBS=-ljemalloc --with-ovs-source=/ovs
fi
make rpm-fedora
